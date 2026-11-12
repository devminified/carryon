import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

test('unauthenticated deep link redirects to sign-in and back after login', async ({
  page,
  context,
}) => {
  await page.goto('/p/00000000-0000-0000-0000-000000000000');
  await expect(page).toHaveURL(/\/login\?next=/);

  // simulate completing sign-in (real GitHub OAuth can't be driven here —
  // see auth-helpers.ts) and confirm the app round-trips back to the
  // originally requested page, not just to "/".
  await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
  await page.goto('/p/00000000-0000-0000-0000-000000000000');
  // not a member of that (nonexistent) project id — RLS-backed 404, not a redirect loop
  await expect(page.getByText(/this page could not be found/i)).toBeVisible();
});

test('an unauthenticated request to /api/health gets the real health JSON, not a login redirect', async ({
  request,
}) => {
  // /api/health's own doc comment says it's deliberately unauthenticated
  // — a monitoring probe or docker-compose's own healthcheck: can't
  // follow a 307 to /login. Found for real: proxy.ts's PUBLIC_PATHS
  // never listed it, so every unauthenticated check silently redirected
  // instead of getting the JSON this endpoint promises.
  const res = await request.get('/api/health', { maxRedirects: 0 });
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(body.status).toBe('ok');
});

test('/login renders dynamically, so its CSP nonce header always matches its own inline scripts', async ({
  request,
}) => {
  // Found for real on Vercel, not reproducible against a bare `next
  // start` (no CDN in front of it): /login has no per-request data
  // dependency, so Next statically prerendered it — one fixed HTML
  // response, with no nonce on its own inline hydration bootstrap
  // script, served from the CDN cache to every visitor. proxy.ts's own
  // CSP header still carries a fresh random nonce on every request, so
  // the two could never match — the browser silently blocked the
  // page's own bootstrap script and it never hydrated past the
  // Suspense fallback (login/page.tsx's `fallback={null}` is exactly
  // why that looked like a blank page with no sign-in button at all).
  // `export const dynamic = 'force-dynamic'` on that page is the fix —
  // this test re-fetches twice and checks the nonce genuinely changes
  // request to request (ruling out a cached body) while always
  // matching its own response's inline scripts.
  const res1 = await request.get('/login');
  const body1 = await res1.text();
  const nonce1 = res1.headers()['content-security-policy']?.match(/nonce-([A-Za-z0-9+/=]+)/)?.[1];
  expect(nonce1).toBeTruthy();
  expect(body1).toContain(nonce1);

  const res2 = await request.get('/login');
  const nonce2 = res2.headers()['content-security-policy']?.match(/nonce-([A-Za-z0-9+/=]+)/)?.[1];
  expect(nonce2).toBeTruthy();
  expect(nonce2).not.toBe(nonce1);
});

test("clicking 'Sign in with GitHub' starts the real Supabase OAuth redirect", async ({ page }) => {
  // CI's local Supabase has no real GitHub OAuth App client secret
  // provisioned (ci.yml's `supabase` job never set one either — this is
  // the first test to actually need one) — this stays a local-only
  // real-verification test rather than asserting against a dummy
  // client_id that would just prove Supabase's own redirect construction,
  // not anything about our app's wiring.
  test.skip(!!process.env.CI, 'no real GitHub OAuth App configured in CI');

  await page.goto('/login');
  // Supabase's /auth/v1/authorize immediately 302s onward, so the only
  // reliably observable waypoint is the final landing page — real
  // github.com, carrying the real configured OAuth App's client_id.
  const nav = page.waitForURL(/^https:\/\/github\.com\//);
  await page.getByRole('button', { name: /sign in with github/i }).click();
  await nav;
  expect(page.url()).toContain('client_id=');
});

test('a signed-in user sees the dashboard shell and can sign out', async ({ page, context }) => {
  await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
  await page.goto('/');

  await expect(page.getByRole('link', { name: 'Carryon' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();

  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login/);

  // signed out for real — a protected route redirects again, not served from a stale cache
  await page.goto('/');
  await expect(page).toHaveURL(/\/login/);
});
