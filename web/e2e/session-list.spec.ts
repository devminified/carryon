import { test, expect, type Page } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// CO-704: session-table.tsx moved onto a real TanStack DataTable —
// looking up a column by its real header text (once, at test time)
// instead of a hardcoded td:nth-child index means a future column
// reorder changes nothing here, rather than silently reading the
// wrong column with no failure at all.
async function columnIndex(page: Page, label: string): Promise<number> {
  const headers = await page.locator('thead th').allTextContents();
  const idx = headers.findIndex((h) => h.trim() === label);
  if (idx === -1) throw new Error(`no "${label}" column header found`);
  return idx;
}

// Seeded directly against local Supabase before this suite runs — see
// e2e/seed.sql (CO-402's own real-verification fixture): project
// 33333333-…, 10,050 real sessions, project 44444444-… with no members.
//
// Serial, not parallel: every test here reads or mutates that one
// shared, order-sensitive dataset (pagination and filtering both
// depend on last_activity_at ordering) — running them concurrently
// raced each other for real (see this PR's description for the actual
// repro), not a flake in the app under test.
test.describe.serial('session list (CO-402)', () => {
  const PERF_PROJECT_ID = '33333333-3333-3333-3333-333333333333';
  const NO_ACCESS_PROJECT_ID = '44444444-4444-4444-4444-444444444444';
  const SUPABASE_URL = 'http://127.0.0.1:54421';
  const SERVICE_ROLE_KEY =
    'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

  test('first page of 10,050 real sessions loads fast and shows the newest first', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    const start = Date.now();
    await page.goto(`/p/${PERF_PROJECT_ID}`);
    await expect(page.getByText('session 1 - fix payment')).toBeVisible();
    // The ticket's own "<1s" is measured directly against the server
    // response (curl against this exact seed: ~350ms) — see the PR
    // description. This assertion includes full browser load/hydration,
    // so it's a generous regression guard, not the headline evidence.
    expect(Date.now() - start).toBeLessThan(8000);
  });

  test('filters narrow the list via the URL', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${PERF_PROJECT_ID}`);

    await page.getByRole('combobox', { name: 'Filter by branch' }).selectOption('main');
    await expect(page).toHaveURL(/branch=main/);
    const branchCol = await columnIndex(page, 'Branch');
    const rows = page.locator('tbody tr');
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      await expect(rows.nth(i).locator('td').nth(branchCol)).toHaveText('main');
    }
  });

  test("pagination's Load more fetches the next 50 with no overlap", async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${PERF_PROJECT_ID}`);

    // The session title is the only link inside any row — no column
    // index needed, this is unambiguous regardless of column order.
    const firstPageTitles = await page.locator('tbody tr a').allTextContents();
    await page.getByRole('link', { name: 'Load more' }).click();
    await expect(page).toHaveURL(/cursor=/);
    const secondPageTitles = await page.locator('tbody tr a').allTextContents();

    expect(firstPageTitles.length).toBeGreaterThan(0);
    expect(secondPageTitles.length).toBeGreaterThan(0);
    expect(secondPageTitles.some((t) => firstPageTitles.includes(t))).toBe(false);
  });

  test('a non-member gets a real 404, not just 404-looking text', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    const response = await page.goto(`/p/${NO_ACCESS_PROJECT_ID}`);
    expect(response?.status()).toBe(404);
  });

  test("a live chunk upload's activity bump reaches the page via Realtime, no reload", async ({
    page,
    context,
  }) => {
    // Its own dedicated row, not one shared with the pagination/filter
    // tests above — this test mutates last_activity_at, which would
    // otherwise reshuffle page-1/page-2 membership out from under them.
    const sessionId = crypto.randomUUID();
    const insertRes = await fetch(`${SUPABASE_URL}/rest/v1/sessions`, {
      method: 'POST',
      headers: {
        apikey: SERVICE_ROLE_KEY,
        Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
        'Content-Type': 'application/json',
        Prefer: 'return=minimal',
      },
      body: JSON.stringify({
        id: sessionId,
        project_id: PERF_PROJECT_ID,
        user_id: '11111111-1111-1111-1111-111111111111',
        original_cwd: '/tmp/co402-realtime',
        branch: 'realtime-test',
        status: 'live',
        title: 'realtime test fixture session',
        last_activity_at: new Date(Date.now() - 60_000).toISOString(),
      }),
    });
    expect(insertRes.ok).toBe(true);

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${PERF_PROJECT_ID}?branch=realtime-test`);

    // Wait for the real Realtime subscription to actually be live
    // before mutating anything — Realtime has no event replay/backlog,
    // so a change published before the channel reaches 'SUBSCRIBED' is
    // simply never delivered, not delivered late. Racing this with a
    // fixed sleep (the previous version of this test) is exactly what
    // made it flake under CI: the WebSocket handshake to a cold local
    // Realtime server can take longer than a developer's own warm
    // local stack, and no amount of waiting *after* the PATCH would
    // recover a missed event. See use-live-sessions.ts's own comment.
    await expect(page.locator('[data-realtime-status]')).toHaveAttribute(
      'data-realtime-status',
      'subscribed',
      { timeout: 15000 },
    );

    const row = page.locator('tbody tr').filter({ hasText: 'realtime test fixture session' });
    const lastActivityCol = await columnIndex(page, 'Last activity');
    const before = await row.locator('td').nth(lastActivityCol).textContent();

    // Real confirm-chunk-style activity bump, via the real PostgREST API —
    // not a UI action, so this proves the Realtime wire-up, not a refetch
    // triggered by something the page itself did.
    const patchRes = await fetch(`${SUPABASE_URL}/rest/v1/sessions?id=eq.${sessionId}`, {
      method: 'PATCH',
      headers: {
        apikey: SERVICE_ROLE_KEY,
        Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
        'Content-Type': 'application/json',
        Prefer: 'return=minimal',
      },
      body: JSON.stringify({ last_activity_at: new Date().toISOString() }),
    });
    expect(patchRes.ok).toBe(true);

    await expect(async () => {
      const after = await row.locator('td').nth(lastActivityCol).textContent();
      expect(after).not.toBe(before);
    }).toPass({ timeout: 5000 });

    // same row, not a reload-and-happen-to-match — title still present
    await expect(row.locator('a')).toHaveText('realtime test fixture session');

    await fetch(`${SUPABASE_URL}/rest/v1/sessions?id=eq.${sessionId}`, {
      method: 'DELETE',
      headers: { apikey: SERVICE_ROLE_KEY, Authorization: `Bearer ${SERVICE_ROLE_KEY}` },
    });
  });
});
