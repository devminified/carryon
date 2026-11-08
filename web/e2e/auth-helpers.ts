import { createServerClient, type CookieOptions } from '@supabase/ssr';
import type { BrowserContext } from '@playwright/test';

// GitHub OAuth's real consent screen can't be driven from Playwright, so
// these tests sign in the same way the Go e2e suite does (CO-211's
// e2e_test.go): a password grant against local Supabase Auth for one of
// seed.sql's real seeded users. local dev has auth.email.enable_signup
// on for exactly this reason. What's actually under test here is our own
// route-protection and session-cookie code (proxy.ts, requireUser,
// sign-out) — not GitHub's sign-in screen, which is out of our hands.
const SUPABASE_URL = 'http://127.0.0.1:54421';
const ANON_KEY = 'sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH';

async function passwordGrant(email: string, password: string) {
  const res = await fetch(`${SUPABASE_URL}/auth/v1/token?grant_type=password`, {
    method: 'POST',
    headers: { apikey: ANON_KEY, 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) {
    throw new Error(`password grant for ${email}: HTTP ${res.status} ${await res.text()}`);
  }
  const { access_token, refresh_token } = await res.json();
  return { access_token, refresh_token };
}

// Drives the real @supabase/ssr cookie serialization (same code our own
// src/shared/lib/supabase/server.ts uses) so the cookies this test
// injects are byte-for-byte what our app would have set itself after a
// real OAuth callback — not a hand-guessed name/shape.
export async function signInAsSeedUser(context: BrowserContext, email: string, password: string) {
  const { access_token, refresh_token } = await passwordGrant(email, password);

  const toSet: { name: string; value: string; options: CookieOptions }[] = [];
  const supabase = createServerClient(SUPABASE_URL, ANON_KEY, {
    cookies: {
      getAll() {
        return [];
      },
      setAll(cookiesToSet) {
        toSet.push(...cookiesToSet);
      },
    },
  });
  const { error } = await supabase.auth.setSession({ access_token, refresh_token });
  if (error) throw error;

  await context.addCookies(
    toSet.map(({ name, value }) => ({
      name,
      value,
      domain: '127.0.0.1',
      path: '/',
    })),
  );
}
