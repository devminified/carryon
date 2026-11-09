import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// Seeded via web/e2e/seed-viewer-fixtures.mjs's CO-404 fixtures (run once
// before this suite, see package.json's test:e2e script).
const FORK_ROOT_SESSION_ID = '88888888-8888-4888-8888-888888888001';
const FORK_CHILD_A_SESSION_ID = '88888888-8888-4888-8888-888888888002';
const FORK_GRANDCHILD_SESSION_ID = '88888888-8888-4888-8888-888888888004';
const FORK_CHAIN_FIRST_ID = '88888888-8888-4888-8888-888888880010';
const FORK_CHAIN_LAST_ID = '88888888-8888-4888-8888-888888880019';

test.describe.serial('fork family tree (CO-404)', () => {
  test('a root session shows its own children and an inaccessible grandchild-sibling without details', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${FORK_ROOT_SESSION_ID}`);

    await expect(page.getByText('Fork history')).toBeVisible();
    await expect(
      page.getByRole('link', { name: /investigate the retry queue race/ }),
    ).toHaveClass(/font-semibold/);
    await expect(page.getByRole('link', { name: /fork: try exponential backoff/ })).toBeVisible();
    await expect(
      page.getByRole('link', { name: /fork: try a circuit breaker instead/ }),
    ).toBeVisible();

    // the hidden-project fork never reveals its title/owner
    await expect(page.getByText("1 fork in a project you can't see")).toBeVisible();
  });

  test('a deep node shows the full ancestor chain up to the root, including the current node highlighted', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto(`/sessions/${FORK_GRANDCHILD_SESSION_ID}`);

    await expect(page.getByRole('link', { name: /investigate the retry queue race/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /fork: try exponential backoff/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /fork: tune the backoff ceiling/ })).toHaveClass(
      /font-semibold/,
    );

    // clicking a parent in the tree navigates to that session's own viewer
    await page.getByRole('link', { name: /fork: try exponential backoff/ }).click();
    await expect(page).toHaveURL(`/sessions/${FORK_CHILD_A_SESSION_ID}`);
  });

  test('a real 10-deep fork chain renders every link from either end', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');

    await page.goto(`/sessions/${FORK_CHAIN_FIRST_ID}`);
    for (let i = 1; i <= 10; i++) {
      await expect(page.getByRole('link', { name: new RegExp(`fork chain link ${i}\\D`) })).toBeVisible();
    }

    await page.goto(`/sessions/${FORK_CHAIN_LAST_ID}`);
    for (let i = 1; i <= 10; i++) {
      await expect(page.getByRole('link', { name: new RegExp(`fork chain link ${i}\\D`) })).toBeVisible();
    }
  });

  test('a session with no forks at all renders no fork history section', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    // the plain, non-forked CO-403 fixture session
    await page.goto('/sessions/77777777-7777-4777-8777-777777777001');
    await expect(page.getByText('Can you fix the payment webhook retry logic?')).toBeVisible();
    await expect(page.getByText('Fork history')).not.toBeVisible();
  });
});
