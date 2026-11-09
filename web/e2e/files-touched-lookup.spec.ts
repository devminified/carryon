import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// Seeded via web/e2e/seed-viewer-fixtures.mjs's CO-408 fixtures (run
// once before this suite, see package.json's test:e2e script) — the
// same SUMMARY_SESSION_ID fixture CO-501's own search test reuses,
// since it's the one real session in this project with a non-empty
// files_touched list to look up.
const RENAME_PROJECT_B_ID = '66666666-6666-6666-6666-666666666406'; // ali: admin

test.describe('"who touched this file" lookup (CO-502)', () => {
  test('an exact file path finds the session that touched it', async ({ page, context }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}?file=/repo/webhook.ts`);

    await expect(page.getByText('fix the payment webhook retry bug')).toBeVisible();
  });

  test('a directory/file prefix with no trailing exact match still finds it', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}?file=/repo/webhook`);

    await expect(page.getByText('fix the payment webhook retry bug')).toBeVisible();
  });

  test('a file nothing touched shows the empty-results message, not an error', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/p/${RENAME_PROJECT_B_ID}?file=/repo/nothing-touches-this.ts`);

    await expect(page.getByText('No sessions match these filters.')).toBeVisible();
  });

  test("a query and a file filter both present in the URL — q wins, matching page.tsx's own branch order", async ({
    page,
    context,
  }) => {
    // Committing a real q triggers a real search Edge Function call
    // (CO-501) — a cold edge-runtime container's first real call can
    // take 30-60s (see docs/co-501-embedding-choice.md), and this spec
    // file can run before summary-display-and-rerun.spec.ts's own
    // test has had a chance to warm it up.
    test.setTimeout(90_000);

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    // Chosen so each filter alone would show a different, checkable
    // outcome: file alone finds nothing (same fixture as the test
    // above); q alone finds the webhook session. Navigating directly
    // with both set — rather than typing into the UI and pressing
    // Enter, which session-filters.tsx's own commitSearch/commitFile
    // already keep mutually exclusive in the URL client-side — tests
    // page.tsx's own server-side branch order directly, with no
    // client-interaction timing dependency of its own.
    await page.goto(
      `/p/${RENAME_PROJECT_B_ID}?file=/repo/nothing-touches-this.ts&q=payment+webhook`,
    );

    await expect(page.getByText('fix the payment webhook retry bug')).toBeVisible();
  });
});
