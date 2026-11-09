import { defineConfig, devices } from '@playwright/test';

// baseURL is 127.0.0.1, not localhost — supabase/config.toml's redirect
// allowlist matches "http://127.0.0.1:3000" exactly (site_url) plus a
// wildcard port range on 127.0.0.1 for the CLI's own loopback flow; it
// does not list "localhost".
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  // Every test here shares one real local Supabase instance and one
  // real Next server process (not mocks) — parallel workers contending
  // for both caused real, reproducible flakes unrelated to the app
  // under test (see this PR's description). Fully serial; this suite
  // is small enough (under a minute total) that the tradeoff is easy.
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:3000',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'pnpm build && pnpm start',
    url: 'http://127.0.0.1:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
