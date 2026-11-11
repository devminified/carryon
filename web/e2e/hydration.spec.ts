import { test, expect, type Page } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// CO-601: a real regression test for the hydration mismatch this
// ticket fixed. formatRelativeTime's old `now = new Date()` default
// was evaluated independently at the server render and again at
// client hydration — a Client Component rendering both passes could
// see two different real instants, producing a React "text content
// did not match" warning. The bug was timing-dependent, not
// always-reproducible on a fast machine, so this asserts on a hard
// reload (a real SSR + hydration cycle, not a client-side
// navigation) repeated several times, not just once.

const PERF_PROJECT_ID = '33333333-3333-3333-3333-333333333333';
const PLAIN_SESSION_ID = '77777777-7777-4777-8777-777777777001';

function isHydrationWarning(text: string): boolean {
  return /hydrat|did not match|text content does not match/i.test(text);
}

async function assertNoHydrationWarnings(page: Page, url: string): Promise<void> {
  const warnings: string[] = [];
  const onConsole = (msg: { type: () => string; text: () => string }) => {
    if (isHydrationWarning(msg.text())) warnings.push(`[console ${msg.type()}] ${msg.text()}`);
  };
  const onPageError = (err: Error) => {
    if (isHydrationWarning(err.message)) warnings.push(`[pageerror] ${err.message}`);
  };
  page.on('console', onConsole);
  page.on('pageerror', onPageError);

  // page.goto is a real hard navigation (a fresh SSR + hydration
  // cycle) — not router.push's client-side navigation, which
  // wouldn't exercise the server-render-then-hydrate path at all.
  await page.goto(url, { waitUntil: 'load' });
  await page.waitForTimeout(500);

  page.off('console', onConsole);
  page.off('pageerror', onPageError);

  expect(warnings, `hydration warning(s) on ${url}`).toEqual([]);
}

test.describe('no hydration mismatches (CO-601)', () => {
  test('hard-reloading /p/[project] five times never produces a hydration warning', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    for (let i = 0; i < 5; i++) {
      await assertNoHydrationWarnings(page, `/p/${PERF_PROJECT_ID}`);
    }
  });

  test('hard-reloading /sessions/[id] five times never produces a hydration warning', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    for (let i = 0; i < 5; i++) {
      await assertNoHydrationWarnings(page, `/sessions/${PLAIN_SESSION_ID}`);
    }
  });
});
