import { test, expect } from '@playwright/test';
import { signInAsSeedUser } from './auth-helpers';

// ali@devminified.com is admin on supabase/seed.sql's own acme-api
// project — that alone already makes her a global admin
// (is_any_project_admin()). sara@devminified.com turns out NOT to be
// a safe "non-admin" fixture — CO-404's own seed-viewer-fixtures.mjs
// makes her admin of FORK_HIDDEN_PROJECT_ID for an unrelated reason,
// found the hard way when the first version of this test asserted
// sara got a 404 and got a real 200 instead. So this spec creates its
// own disposable, genuinely-admin-nowhere user instead of reusing a
// shared fixture persona for that one property.

const SUPABASE_URL = 'http://127.0.0.1:54421';
const SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

const UNIQUE = Date.now();
const NEW_PROJECT_REMOTE = `github.com/co410-e2e/client-${UNIQUE}`;
const NEW_PROJECT_NAME = `co410-e2e-client-${UNIQUE}`;
const SARA_USER_ID = '22222222-2222-2222-2222-222222222222';
const SARA_EMAIL = 'sara@devminified.com';

async function restGet(path: string) {
  const res = await fetch(`${SUPABASE_URL}/rest/v1/${path}`, {
    headers: { apikey: SERVICE_ROLE_KEY, Authorization: `Bearer ${SERVICE_ROLE_KEY}` },
  });
  return res.json();
}

async function restUpsert(table: string, body: unknown, onConflict: string) {
  const res = await fetch(`${SUPABASE_URL}/rest/v1/${table}?on_conflict=${onConflict}`, {
    method: 'POST',
    headers: {
      apikey: SERVICE_ROLE_KEY,
      Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
      'Content-Type': 'application/json',
      Prefer: 'resolution=merge-duplicates',
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`upsert ${table}: HTTP ${res.status} ${await res.text()}`);
}

async function restInsert(table: string, body: unknown) {
  const res = await fetch(`${SUPABASE_URL}/rest/v1/${table}`, {
    method: 'POST',
    headers: {
      apikey: SERVICE_ROLE_KEY,
      Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
      'Content-Type': 'application/json',
      Prefer: 'return=minimal',
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`insert ${table}: HTTP ${res.status} ${await res.text()}`);
}

async function findProjectId(name: string): Promise<string> {
  const rows = await restGet(`projects?name=eq.${name}&select=id`);
  if (rows.length === 0) throw new Error(`project ${name} not found`);
  return rows[0].id;
}

const ANON_KEY = 'sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH';

// signInAsSeedUser (auth-helpers.ts) only sets browser cookies — it
// doesn't hand back the access token this test needs to call an Edge
// Function directly, the same real password-grant call it uses under
// the hood.
async function accessTokenFor(email: string, password: string): Promise<string> {
  const res = await fetch(`${SUPABASE_URL}/auth/v1/token?grant_type=password`, {
    method: 'POST',
    headers: { apikey: ANON_KEY, 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error(`password grant: HTTP ${res.status} ${await res.text()}`);
  const { access_token } = await res.json();
  return access_token;
}

const NON_ADMIN_EMAIL = `co410-non-admin-${UNIQUE}@devminified.com`;
const NON_ADMIN_PASSWORD = 'password123';

async function createDisposableNonAdminUser(): Promise<void> {
  const res = await fetch(`${SUPABASE_URL}/auth/v1/admin/users`, {
    method: 'POST',
    headers: {
      apikey: SERVICE_ROLE_KEY,
      Authorization: `Bearer ${SERVICE_ROLE_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      email: NON_ADMIN_EMAIL,
      password: NON_ADMIN_PASSWORD,
      email_confirm: true,
      user_metadata: { user_name: 'co410-non-admin' },
    }),
  });
  if (!res.ok) throw new Error(`creating disposable user: HTTP ${res.status} ${await res.text()}`);
}

test.describe.serial('admin pages (CO-410)', () => {
  test('a non-admin gets a real 404 on every admin route', async ({ page, context }) => {
    await createDisposableNonAdminUser();
    await signInAsSeedUser(context, NON_ADMIN_EMAIL, NON_ADMIN_PASSWORD);
    for (const path of [
      '/admin',
      '/admin/projects',
      '/admin/members',
      '/admin/audit',
      '/admin/agents',
      '/admin/config',
    ]) {
      const res = await page.goto(path);
      expect(res?.status()).toBe(404);
    }
  });

  test('a global admin does not see the Admin link gated for non-admins, and can reach the Projects page', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();

    await page.goto('/admin');
    await expect(page).toHaveURL(/\/admin\/projects$/);
    await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
  });

  test('an admin can register a new client project end to end, under 5 minutes of real interaction', async ({
    page,
    context,
  }) => {
    const startedAt = Date.now();
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/projects');

    await page.getByLabel('Name').fill(NEW_PROJECT_NAME);
    await page.getByLabel('Git remote (host/owner/repo)').fill(NEW_PROJECT_REMOTE);
    await page.getByRole('button', { name: 'Register project' }).click();

    await expect(page.getByText('Project registered.')).toBeVisible();
    const row = page.getByRole('list').locator('li').filter({ hasText: NEW_PROJECT_NAME });
    await expect(row).toBeVisible();
    await expect(row.getByText(NEW_PROJECT_REMOTE)).toBeVisible();

    expect(Date.now() - startedAt).toBeLessThan(5 * 60 * 1000);
  });

  test('registering the same remote twice is rejected with a clean conflict message', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/projects');

    await page.getByLabel('Name').fill(NEW_PROJECT_NAME);
    await page.getByLabel('Git remote (host/owner/repo)').fill(NEW_PROJECT_REMOTE);
    await page.getByRole('button', { name: 'Register project' }).click();

    await expect(page.getByText(/already registered/)).toBeVisible();
  });

  test("an admin can edit the new project's settings, and they persist across a reload", async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/projects');

    const row = page.locator('li').filter({ hasText: NEW_PROJECT_NAME });
    await row.locator('summary').click();
    await row.getByLabel('Dedicated bucket').fill('client-dedicated-bucket');
    await row.getByLabel('Retention (days after close)').fill('45');
    await row.getByLabel('Daily AI token budget').fill('50000');
    await row
      .getByLabel('Slack webhook URL')
      .fill('https://hooks.slack.com/services/T0/B0/co410e2e');
    // Follow-up to CO-409 (#113): the Mattermost field is independent
    // of Slack's — both can be set at once, same reasoning the
    // migration and Edge Function fan-out already apply.
    await row
      .getByLabel('Mattermost webhook URL')
      .fill('https://mattermost.example/hooks/co410e2e');
    await row.getByLabel('Daily digest').check();
    await row.getByRole('button', { name: 'Save settings' }).click();
    await expect(row.getByText('Saved.')).toBeVisible();

    await page.reload();
    const reloadedRow = page.locator('li').filter({ hasText: NEW_PROJECT_NAME });
    await reloadedRow.locator('summary').click();
    await expect(reloadedRow.getByLabel('Dedicated bucket')).toHaveValue('client-dedicated-bucket');
    await expect(reloadedRow.getByLabel('Retention (days after close)')).toHaveValue('45');
    await expect(reloadedRow.getByLabel('Daily AI token budget')).toHaveValue('50000');
    await expect(reloadedRow.getByLabel('Slack webhook URL')).toHaveValue(
      'https://hooks.slack.com/services/T0/B0/co410e2e',
    );
    await expect(reloadedRow.getByLabel('Mattermost webhook URL')).toHaveValue(
      'https://mattermost.example/hooks/co410e2e',
    );
    await expect(reloadedRow.getByLabel('Daily digest')).toBeChecked();
  });

  test('the Admin nav link stays hidden for a non-admin', async ({ page, context }) => {
    await signInAsSeedUser(context, NON_ADMIN_EMAIL, NON_ADMIN_PASSWORD);
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Admin' })).toHaveCount(0);
  });

  test('an admin can add a manual member override, change her role, then remove her', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    const projectId = await findProjectId(NEW_PROJECT_NAME);

    await page.goto(`/admin/members?project=${projectId}`);
    await page.getByLabel('Email').fill(SARA_EMAIL);
    await page.getByLabel('Role').selectOption('viewer');
    await page.getByRole('button', { name: 'Add manual override' }).click();

    const row = page.locator('tr').filter({ hasText: 'sara' });
    await expect(row).toBeVisible();
    await expect(row.getByText('manual')).toBeVisible();

    await row.locator('select[name="role"]').selectOption('resumer');
    // The role select auto-submits a real Server Action on change —
    // wait for the write to actually land before reloading, rather
    // than an arbitrary sleep.
    await expect
      .poll(async () => {
        const rows = await restGet(
          `project_members?project_id=eq.${projectId}&user_id=eq.${SARA_USER_ID}&select=role`,
        );
        return rows[0]?.role;
      })
      .toBe('resumer');

    await page.reload();
    const reloadedRow = page.locator('tr').filter({ hasText: 'sara' });
    await expect(reloadedRow.locator('select[name="role"]')).toHaveValue('resumer');

    await reloadedRow.getByRole('button', { name: 'Remove' }).click();
    await expect(page.locator('tr').filter({ hasText: 'sara' })).toHaveCount(0);
  });

  test('the audit log shows real rows for the actions above, and CSV export matches the filtered view', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    const projectId = await findProjectId(NEW_PROJECT_NAME);

    await page.goto(`/admin/audit?projectId=${projectId}`);
    await expect(page.getByRole('cell', { name: 'project_settings_changed' })).toBeVisible();
    // Three role_change rows: the manual add, the role update, the
    // removal — all against the same project, from the previous test.
    await expect(page.getByRole('cell', { name: 'role_change' }).first()).toBeVisible();
    await expect(page.getByRole('cell', { name: 'role_change' })).toHaveCount(3);
    await expect(page.getByText('ali').first()).toBeVisible();

    const exportRes = await page.request.get(`/api/admin/audit/export?projectId=${projectId}`);
    expect(exportRes.status()).toBe(200);
    expect(exportRes.headers()['content-type']).toContain('text/csv');
    const csv = await exportRes.text();
    const lines = csv.trim().split('\n');
    expect(lines[0]).toBe('at,action,actor,actorLogin,projectId,projectName,sessionId,reason');
    expect(lines.length - 1).toBeGreaterThanOrEqual(2); // project_settings_changed + at least one role_change
    expect(csv).toContain('project_settings_changed');
    expect(csv).toContain(projectId);
  });

  test('the agents page shows a seeded heartbeat with the right liveness status', async ({
    page,
    context,
  }) => {
    const now = new Date();
    const onlineAt = new Date(now.getTime() - 60_000).toISOString(); // 1 minute ago
    const staleAt = new Date(now.getTime() - 20 * 60_000).toISOString(); // 20 minutes ago
    await restUpsert(
      'agent_heartbeats',
      {
        user_id: SARA_USER_ID,
        machine_id: 'e2e-online',
        hostname: 'saras-laptop',
        version: '9.9.9',
        blocked_sessions: 0,
        last_seen_at: onlineAt,
        os: 'darwin',
        bytes_behind: 2048,
        upload_attempts: 10,
        upload_errors: 1,
        redactions_applied: 3,
      },
      'user_id,machine_id',
    );
    await restUpsert(
      'agent_heartbeats',
      {
        user_id: SARA_USER_ID,
        machine_id: 'e2e-stale',
        hostname: 'saras-desktop',
        version: '9.9.8',
        blocked_sessions: 2,
        last_seen_at: staleAt,
      },
      'user_id,machine_id',
    );

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/agents');

    const onlineRow = page.locator('tr').filter({ hasText: 'saras-laptop' });
    await expect(onlineRow.getByText('Online')).toBeVisible();
    // CO-504's own new columns, on the real seeded row above.
    await expect(onlineRow.getByText('darwin')).toBeVisible();
    await expect(onlineRow.getByText('2.0 KB')).toBeVisible();

    const staleRow = page.locator('tr').filter({ hasText: 'saras-desktop' });
    await expect(staleRow.getByText('Stale')).toBeVisible();
    await expect(staleRow.getByRole('cell', { name: '2', exact: true })).toBeVisible();
    // An older agent's row (no CO-504 fields sent yet) shows an
    // honest "—" for OS, not a blank cell or a stale default.
    await expect(staleRow.getByText('—').first()).toBeVisible();
  });

  test('a signed-in user (sara) reaches the real heartbeat Edge Function end to end, and it shows up on the Agents page', async ({
    page,
    context,
  }) => {
    // The real wire path cmd/carryon's runHeartbeatLoop uses, driven
    // by a real access token — not a service-role insert like the
    // fixture above.
    const token = await accessTokenFor(SARA_EMAIL, 'password123');
    const res = await fetch(`${SUPABASE_URL}/functions/v1/heartbeat`, {
      method: 'POST',
      headers: {
        apikey: ANON_KEY,
        Authorization: `Bearer ${token}`,
        'content-type': 'application/json',
      },
      body: JSON.stringify({
        machineId: 'e2e-real-call',
        hostname: 'real-heartbeat-host',
        version: '1.0.0',
        blockedSessions: 0,
      }),
    });
    expect(res.status).toBe(200);

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/agents');
    await expect(page.getByText('real-heartbeat-host')).toBeVisible();
  });

  test('the observability dashboard (CO-504) shows a real error rate derived from the heartbeats seeded above', async ({
    page,
    context,
  }) => {
    // Depends on test.describe.serial — the fleet-wide
    // fleet_upload_error_rate() this page reads sums upload_attempts/
    // upload_errors across every heartbeat row in the database,
    // including e2e-online (10 attempts, 1 error) seeded by the first
    // test in this file: 1/10 = 10.0%, the exact same SQL function
    // check-observability-alerts itself would use.
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/dashboard');

    await expect(page.getByText('Fleet upload error rate')).toBeVisible();
    await expect(page.getByText('10.0%')).toBeVisible();
  });

  test('the observability dashboard (CO-504) shows real summary job latency and failure rate', async ({
    page,
    context,
  }) => {
    // ai_usage only ever logs a successful Anthropic call — this is
    // the one real data source for "summary job latency and
    // failures" (the ticket's own wording), seeded directly since
    // nothing else in this suite drives a real summarizeSession() run.
    await restInsert('summarize_job_runs', [
      { status: 'summarized', duration_ms: 1000 },
      { status: 'summarized', duration_ms: 1000 },
      { status: 'summarized', duration_ms: 10000 },
      { status: 'gave_up', duration_ms: 50 },
    ]);

    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/dashboard');

    await expect(page.getByText('Summary job latency (p50 / p95)')).toBeVisible();
    await expect(page.getByText('Summary job failure rate, last 24h')).toBeVisible();
    // Only the 3 'summarized' durations count for latency (a gave_up
    // run's own duration is dominated by whatever made it bail
    // early, not real summarization time): percentile_cont(0.5) of
    // [1000, 1000, 10000] = 1000ms = 1s; percentile_cont(0.95) =
    // 9100ms, rounds to 9s. Failure rate counts gave_up over every
    // run (3 summarized + 1 gave_up) = 25.0%.
    await expect(page.getByText('1s / 9s')).toBeVisible();
    await expect(page.getByText('25.0%')).toBeVisible();
  });

  test('an admin can update the redaction config, and it persists across a reload', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/config');

    await page.getByLabel(/Disabled rule IDs/).fill('generic-api-key\nmailgun-api-key');
    await page.getByLabel(/Extra path denylist globs/).fill('**/co410-e2e-secrets/**');
    await page.getByLabel(/Pinned agent version/).fill('1.4.0');
    await page.getByRole('button', { name: 'Save config' }).click();
    await expect(page.getByText('Saved.')).toBeVisible();

    await page.reload();
    await expect(page.getByLabel(/Disabled rule IDs/)).toHaveValue(
      'generic-api-key\nmailgun-api-key',
    );
    await expect(page.getByLabel(/Extra path denylist globs/)).toHaveValue(
      '**/co410-e2e-secrets/**',
    );
    await expect(page.getByLabel(/Pinned agent version/)).toHaveValue('1.4.0');

    const configRows = await restGet('redaction_config?id=eq.default&select=disabled_rule_ids');
    expect(configRows[0].disabled_rule_ids).toEqual(['generic-api-key', 'mailgun-api-key']);

    await page.goto('/admin/audit');
    await expect(page.getByRole('cell', { name: 'config_change' })).toBeVisible();
  });

  test('an admin cannot pin a version with no green Claude Code compatibility run (CO-503)', async ({
    page,
    context,
  }) => {
    await signInAsSeedUser(context, 'ali@devminified.com', 'password123');
    await page.goto('/admin/config');

    await page.getByLabel(/Pinned agent version/).fill('9.9.9-no-compat-run');
    await page.getByRole('button', { name: 'Save config' }).click();

    await expect(page.getByText(/no green Claude Code compatibility run/)).toBeVisible();

    const configRows = await restGet('redaction_config?id=eq.default&select=pinned_version');
    expect(configRows[0].pinned_version).not.toBe('9.9.9-no-compat-run');
  });
});
