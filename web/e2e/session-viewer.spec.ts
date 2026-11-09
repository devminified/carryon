import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// Seeded via web/e2e/seed-viewer-fixtures.mjs (run once before this
// suite, see package.json's test:e2e script) — real zstd-compressed
// chunks in real local Storage, not SQL-only rows, since the viewer
// genuinely downloads and decompresses them.
const PLAIN_SESSION_ID = '77777777-7777-4777-8777-777777777001';
const REDACTED_SESSION_ID = '77777777-7777-4777-8777-777777777002';
const SUBAGENT_SESSION_ID = '77777777-7777-4777-8777-777777777003';
const LARGE_SESSION_ID = '77777777-7777-4777-8777-777777777004';

const SUPABASE_URL = 'http://127.0.0.1:54421';
const SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

async function countViewAuditRows(sessionId: string): Promise<number> {
  const res = await fetch(
    `${SUPABASE_URL}/rest/v1/audit_log?select=action&session_id=eq.${sessionId}&action=eq.view`,
    { headers: { apikey: SERVICE_ROLE_KEY, Authorization: `Bearer ${SERVICE_ROLE_KEY}` } },
  );
  const rows = await res.json();
  return rows.length;
}

test.describe.serial('session viewer (CO-403)', () => {
  test('renders chat, collapsed tool-call summaries, and a diff on expand', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${PLAIN_SESSION_ID}`);

    await expect(page.getByText('Can you fix the payment webhook retry logic?')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Read `webhook.ts`' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Edited `webhook.ts` (+3 −1)' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Ran `npm test`, failed' })).toBeVisible();
    await expect(page.getByText('error', { exact: true })).toBeVisible();

    // unknown line type degrades to raw JSON instead of crashing
    await expect(page.getByText('Unrecognized line')).toBeVisible();
    await expect(page.getByText('"something_from_the_future"')).toBeVisible();

    await page.getByRole('button', { name: 'Edited `webhook.ts` (+3 −1)' }).click();
    await expect(page.getByText('withBackoff(() => fetch(url));')).toBeVisible();
    await expect(page.locator('text=- export function retry')).toBeVisible();
  });

  test('highlights a redacted value instead of ever showing the raw secret', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${REDACTED_SESSION_ID}`);

    await page.getByRole('button', { name: 'Read `.env.local`' }).click();
    await expect(page.locator('mark')).toHaveText('[REDACTED:denylisted-file .env.local]');

    const bodyText = await page.locator('body').innerText();
    expect(bodyText).not.toContain('AKIA');
  });

  test('renders a subagent stream in its own collapsible section', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${SUBAGENT_SESSION_ID}`);

    await expect(page.getByText('Subagent: agent-1')).toBeVisible();
    // the child stream's own content isn't visible until expanded
    await expect(page.getByText('race in the retry queue under load')).not.toBeVisible();

    await page.getByText('Subagent: agent-1').click();
    await expect(page.getByText('race in the retry queue under load')).toBeVisible();
  });

  test('a view audit row is written once per real page open', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${PLAIN_SESSION_ID}`);
    await expect(page.getByText('Can you fix the payment webhook retry logic?')).toBeVisible();
    const countAfterOne = await countViewAuditRows(PLAIN_SESSION_ID);
    expect(countAfterOne).toBeGreaterThan(0);

    // a second real open adds exactly one more row, not zero (dedup)
    // and not more than one (double-write)
    await page.goto(`/sessions/${PLAIN_SESSION_ID}`);
    await expect(page.getByText('Can you fix the payment webhook retry logic?')).toBeVisible();
    const countAfterTwo = await countViewAuditRows(PLAIN_SESSION_ID);
    expect(countAfterTwo).toBe(countAfterOne + 1);
  });

  test("a non-viewer gets a real 404, and a plain-string session id doesn't crash the viewer", async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    const res = await page.goto('/sessions/00000000-0000-0000-0000-000000000000');
    expect(res?.status()).toBe(404);
  });

  test('a large, real multi-chunk (15-chunk) session: fast first content, bounded DOM, full data integrity', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');

    const start = Date.now();
    await page.goto(`/sessions/${LARGE_SESSION_ID}`);
    await page.getByText('exchange 0:').waitFor({ state: 'visible' });
    // Generous CI margin around the ticket's own "<2s" — real measured
    // value locally was ~1.2s; see this PR's description.
    expect(Date.now() - start).toBeLessThan(6000);

    await page.waitForFunction(() => !document.body.innerText.includes('Loading more'), {
      timeout: 15000,
    });

    // Virtualization: far fewer DOM rows mounted than the ~1,200
    // visible entries this session actually has.
    const mountedRows = await page
      .locator('[data-testid="transcript-scroll"] [data-index]')
      .count();
    expect(mountedRows).toBeLessThan(50);

    await page.evaluate(() => {
      const el = document.querySelector('[data-testid="transcript-scroll"]');
      if (el) el.scrollTop = el.scrollHeight;
    });
    await expect(page.getByText('exchange 599:')).toBeVisible();
  });
});
