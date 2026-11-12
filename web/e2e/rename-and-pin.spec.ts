import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// Seeded via web/e2e/seed-viewer-fixtures.mjs's CO-405 fixtures (run
// once before this suite, see package.json's test:e2e script).
const RENAME_PROJECT_A_ID = '66666666-6666-6666-6666-666666666405';
const RENAME_OWNED_BY_ALI_ID = '88888888-8888-4888-8888-888888888405';
const RENAME_OWNED_BY_SARA_ID = '88888888-8888-4888-8888-888888888406';

const RENAME_PROJECT_B_ID = '66666666-6666-6666-6666-666666666406';
const PIN_ADMIN_TARGET_ID = '88888888-8888-4888-8888-888888888409';

const SUPABASE_URL = 'http://127.0.0.1:54421';
const SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

async function getSession(id: string): Promise<{ title: string; title_source: string }> {
  const res = await fetch(`${SUPABASE_URL}/rest/v1/sessions?id=eq.${id}&select=title,title_source`, {
    headers: { apikey: SERVICE_ROLE_KEY, Authorization: `Bearer ${SERVICE_ROLE_KEY}` },
  });
  const rows = await res.json();
  return rows[0];
}

async function getSessionPinned(id: string): Promise<boolean> {
  const res = await fetch(`${SUPABASE_URL}/rest/v1/sessions?id=eq.${id}&select=pinned`, {
    headers: { apikey: SERVICE_ROLE_KEY, Authorization: `Bearer ${SERVICE_ROLE_KEY}` },
  });
  const rows = await res.json();
  return rows[0]?.pinned;
}

test.describe.serial('rename and pin (CO-405)', () => {
  test('the owner can rename their own session, even with only viewer role on the project', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${RENAME_OWNED_BY_ALI_ID}`);

    const titleInput = page.getByRole('textbox').first();
    await expect(titleInput).toHaveValue('ali owns this, viewer role only');
    await titleInput.fill('renamed by its own owner');
    await page.getByRole('button', { name: 'Rename' }).click();
    await expect(page.getByRole('button', { name: 'Rename' })).toBeVisible();

    const row = await getSession(RENAME_OWNED_BY_ALI_ID);
    expect(row.title).toBe('renamed by its own owner');
    expect(row.title_source).toBe('manual');
  });

  test('a mere viewer who is not the owner sees no rename control at all', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${RENAME_OWNED_BY_SARA_ID}`);

    await expect(page.getByText("sara's session, ali is a mere viewer here")).toBeVisible();
    await expect(page.getByRole('textbox')).toHaveCount(0);
  });

  test('a project admin can rename a session they do not own', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${PIN_ADMIN_TARGET_ID}`);

    const titleInput = page.getByRole('textbox').first();
    await expect(titleInput).toHaveValue('quarterly infra review');
    await titleInput.fill('payment architecture decision');
    await page.getByRole('button', { name: 'Rename' }).click();
    await expect(page.getByRole('button', { name: 'Rename' })).toBeVisible();

    const row = await getSession(PIN_ADMIN_TARGET_ID);
    expect(row.title).toBe('payment architecture decision');
    expect(row.title_source).toBe('manual');
  });

  test('a non-admin viewer sees the pin icon on a pinned session, but no toggle control', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_A_ID}`);

    await expect(page.getByLabel('Pinned session')).toBeVisible();
    await expect(page.getByLabel('Unpin session')).toHaveCount(0);
    await expect(page.getByLabel('Pin session', { exact: true })).toHaveCount(0);

    // pinned-first: the pre-pinned (older) session still sorts above
    // the newer unpinned one.
    const firstTitleCell = page.locator('tbody tr td:nth-child(2) a').first();
    await expect(firstTitleCell).toHaveText('pre-pinned session');
  });

  test('an admin can pin a session, and it immediately sorts to the top', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}`);

    // Before pinning: the newer session (ali's own) sorts first.
    await expect(page.locator('tbody tr td:nth-child(2) a').first()).toHaveText(
      'newer unrelated session',
    );

    const targetRow = page.locator('tbody tr').filter({ hasText: 'payment architecture decision' });
    await targetRow.getByLabel('Pin session', { exact: true }).click();

    await expect(page.locator('tbody tr td:nth-child(2) a').first()).toHaveText(
      'payment architecture decision',
    );
    await expect(targetRow.getByLabel('Unpin session')).toBeVisible();

    // unpin again, leaving fixture state clean for a re-run of this suite
    await targetRow.getByLabel('Unpin session').click();
    await expect(targetRow.getByLabel('Pin session', { exact: true })).toBeVisible();
    // CO-607: the label above is now driven by an optimistic update —
    // it flips before the real write necessarily lands. The next test
    // navigates fresh and depends on the server already agreeing it's
    // unpinned, so wait for that explicitly rather than trusting this
    // tab's own (now instant) UI as a proxy for the write having
    // committed.
    await expect.poll(() => getSessionPinned(PIN_ADMIN_TARGET_ID)).toBe(false);
  });

  // CO-607: a tight assertion timeout (well under any real round trip
  // to the local Supabase instance — every other test in this file
  // takes hundreds of ms to low seconds once the real write lands) is
  // what actually distinguishes an optimistic, client-applied update
  // from one that merely happens to be fast. If this ever regressed
  // to waiting for the real response first, this assertion would
  // start timing out well before the 150ms budget below.
  test('pinning flips the icon optimistically, before the real write could plausibly land', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}`);

    const targetRow = page.locator('tbody tr').filter({ hasText: 'payment architecture decision' });
    await targetRow.getByLabel('Pin session', { exact: true }).click();
    await expect(targetRow.getByLabel('Unpin session')).toBeVisible({ timeout: 150 });

    // leave fixture state clean for a re-run of this suite — waiting
    // for the real write (not just this tab's own optimistic label)
    // before the test ends, same reasoning as the previous test.
    await targetRow.getByLabel('Unpin session').click();
    await expect(targetRow.getByLabel('Pin session', { exact: true })).toBeVisible({ timeout: 150 });
    await expect.poll(() => getSessionPinned(PIN_ADMIN_TARGET_ID)).toBe(false);
  });
});
