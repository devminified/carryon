import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// Seeded via web/e2e/seed-viewer-fixtures.mjs's CO-408 fixtures (run
// once before this suite, see package.json's test:e2e script).
const RENAME_PROJECT_B_ID = '66666666-6666-6666-6666-666666666406'; // ali: admin
const RENAME_PROJECT_A_ID = '66666666-6666-6666-6666-666666666405'; // ali: viewer
const SUMMARY_SESSION_ID = '88888888-8888-4888-8888-888888888411';

const SUPABASE_URL = 'http://127.0.0.1:54421';
const SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

async function resetRateLimitMarker(sessionId: string) {
  await fetch(`${SUPABASE_URL}/rest/v1/sessions?id=eq.${sessionId}`, {
    method: 'PATCH',
    headers: {
      apikey: SERVICE_ROLE_KEY,
      Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ last_resummarize_requested_at: null }),
  });
}

test.describe.serial('summary display and re-run (CO-408)', () => {
  test('the list shows the summary, an AI-generated time, files touched, and the outcome badge', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}`);

    const row = page.locator('tbody tr').filter({ hasText: 'fix the payment webhook retry bug' });
    await expect(row.getByText(/The developer asked to fix a flaky payment webhook retry/)).toBeVisible();
    await expect(row.getByText(/AI-generated/)).toBeVisible();
    await expect(row.getByText(/2 files/)).toBeVisible();
    await expect(row.getByText('Done')).toBeVisible();
  });

  test('a blocked session shows its blocked reason as a tooltip on the outcome badge', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_A_ID}`);

    const row = page.locator('tbody tr').filter({ hasText: 'investigate the retry queue blocker' });
    const badge = row.getByText('Blocked');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveAttribute('title', 'waiting on new API credentials');
  });

  test('a non-admin viewer sees the summary but no Re-summarize control', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_A_ID}`);

    const row = page.locator('tbody tr').filter({ hasText: 'investigate the retry queue blocker' });
    await expect(row.getByText(/stuck retry queue/)).toBeVisible();
    await expect(row.getByRole('button', { name: 'Re-summarize' })).toHaveCount(0);
  });

  test('the session detail page shows the same summary, AI-generated time, and files touched', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${SUMMARY_SESSION_ID}`);

    await expect(page.getByText(/The developer asked to fix a flaky payment webhook retry/)).toBeVisible();
    await expect(page.getByText(/AI-generated/)).toBeVisible();
    await expect(page.getByText('/repo/webhook.ts')).toBeVisible();
    await expect(page.getByText('/repo/webhook.test.ts')).toBeVisible();
  });

  test('an admin can click Re-summarize, and a second click within a minute is rate-limited', async ({
    page,
    context,
  }) => {
    await resetRateLimitMarker(SUMMARY_SESSION_ID);
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}`);

    const row = page.locator('tbody tr').filter({ hasText: 'fix the payment webhook retry bug' });
    const button = row.getByRole('button', { name: 'Re-summarize' });
    await expect(button).toBeVisible();

    await button.click();
    // The real Edge Function call completes quickly either way in this
    // local environment (no real ANTHROPIC_API_KEY configured, so the
    // real call 500s immediately — see this PR's own description for
    // why a real golden-run against the live model stays a deliberate,
    // separate follow-up) — what's under test here is the rate limit
    // itself, not a successful AI response.
    await expect(button).toBeVisible({ timeout: 10_000 });

    await button.click();
    await expect(row.getByText(/Already re-summarizing/)).toBeVisible({ timeout: 10_000 });
  });

  test('search finds a session by a phrase from its AI summary', async ({ page, context }) => {
    // CO-501: the search box now calls the real `search` Edge Function
    // (gte-small embeds the query, then the real search_sessions SQL
    // function ranks sessions) — on a freshly started edge runtime
    // (every CI run, and any local `supabase functions serve` restart)
    // the embedding model's first real call anywhere in the suite can
    // cold-start for 30-60s (measured directly, see
    // docs/co-501-embedding-choice.md). The default 30s test timeout
    // isn't enough to tell a real cold start apart from a real bug.
    test.setTimeout(90_000);

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    // navigation's own timeout is separate from the test timeout above
    // — explicit here so a real cold start doesn't trip it either.
    await page.goto(`/p/${RENAME_PROJECT_B_ID}?q=flaky+payment+webhook`, { timeout: 90_000 });

    await expect(page.getByText('fix the payment webhook retry bug')).toBeVisible();
  });
});
