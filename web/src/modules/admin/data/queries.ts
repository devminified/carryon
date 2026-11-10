import 'server-only';
import { createAdminClient } from '@/shared/lib/supabase/admin';
import type {
  AuditLogEntry,
  HeartbeatWithLogin,
  MemberWithLogin,
  ObservabilityDashboard,
  ProjectRow,
  ProjectWithSettings,
  RedactionConfigRow,
} from '../types';

// Every query here runs on the service-role client — requireGlobalAdmin()
// has already confirmed the caller is an admin of at least one project
// before any of these are ever called (every admin page's own
// `await requireGlobalAdmin()` at the top), so this is architecture.md's
// case (b) ("admin operations that can't be expressed via RLS"), not a
// bypass of a check that should have run. See the CO-410 migration's
// own comment for why per-project RLS can't express "manage the whole
// fleet" at all.

async function resolveLogins(
  userIds: string[],
): Promise<Map<string, { login: string | null; email: string | null }>> {
  const admin = createAdminClient();
  const unique = [...new Set(userIds)];
  const entries = await Promise.all(
    unique.map(async (id) => {
      const { data, error } = await admin.auth.admin.getUserById(id);
      if (error || !data.user) {
        return [id, { login: null, email: null }] as const;
      }
      return [
        id,
        {
          login: (data.user.user_metadata?.user_name as string | undefined) ?? null,
          email: data.user.email ?? null,
        },
      ] as const;
    }),
  );
  return new Map(entries);
}

export async function listAllProjects(): Promise<ProjectWithSettings[]> {
  const admin = createAdminClient();
  const { data: projects, error } = await admin
    .from('projects')
    .select('*')
    .order('name', { ascending: true });
  if (error) throw error;

  const { data: settingsRows, error: settingsErr } = await admin
    .from('project_notification_settings')
    .select('project_id, slack_webhook_url, notify_digest');
  if (settingsErr) throw settingsErr;

  const settingsByProject = new Map(
    (settingsRows ?? []).map((s) => [
      s.project_id,
      { slackWebhookUrl: s.slack_webhook_url, notifyDigest: s.notify_digest },
    ]),
  );

  return (projects as ProjectRow[]).map((p) => ({
    ...p,
    slackWebhookUrl: settingsByProject.get(p.id)?.slackWebhookUrl ?? null,
    notifyDigest: settingsByProject.get(p.id)?.notifyDigest ?? false,
  }));
}

export async function listProjectsForPicker(): Promise<Pick<ProjectRow, 'id' | 'name'>[]> {
  const admin = createAdminClient();
  const { data, error } = await admin
    .from('projects')
    .select('id, name')
    .order('name', { ascending: true });
  if (error) throw error;
  return data;
}

export async function listMembersForProject(projectId: string): Promise<MemberWithLogin[]> {
  const admin = createAdminClient();
  const { data, error } = await admin
    .from('project_members')
    .select('*')
    .eq('project_id', projectId)
    .order('role', { ascending: false });
  if (error) throw error;

  const logins = await resolveLogins(data.map((m) => m.user_id));
  return data.map((m) => ({
    ...m,
    login: logins.get(m.user_id)?.login ?? null,
    email: logins.get(m.user_id)?.email ?? null,
  }));
}

export type AuditLogFilters = {
  actorEmail?: string;
  action?: string;
  projectId?: string;
  since?: string;
  until?: string;
};

const AUDIT_PAGE_SIZE = 200;

export async function listAuditLog(filters: AuditLogFilters): Promise<AuditLogEntry[]> {
  const admin = createAdminClient();

  let actorId: string | null = null;
  if (filters.actorEmail) {
    // Same bounded listUsers()-and-filter approach as addMemberOverrideAction
    // — no direct "get user by email" in the admin API, and a small
    // team's user base makes this an honest MVP, not a scale risk.
    const { data: usersPage } = await admin.auth.admin.listUsers({ perPage: 1000 });
    actorId =
      usersPage.users.find((u) => u.email?.toLowerCase() === filters.actorEmail!.toLowerCase())
        ?.id ?? null;
    if (!actorId) return []; // no such user — an honest empty result, not every row
  }

  let query = admin
    .from('audit_log')
    .select('*')
    .order('at', { ascending: false })
    .limit(AUDIT_PAGE_SIZE);
  if (actorId) query = query.eq('actor', actorId);
  if (filters.action) query = query.eq('action', filters.action);
  if (filters.projectId) query = query.eq('project_id', filters.projectId);
  if (filters.since) query = query.gte('at', filters.since);
  if (filters.until) query = query.lte('at', filters.until);

  const { data, error } = await query;
  if (error) throw error;

  const actorIds = data.map((r) => r.actor).filter((a): a is string => a !== null);
  const logins = await resolveLogins(actorIds);

  const projectIds = [
    ...new Set(data.map((r) => r.project_id).filter((p): p is string => p !== null)),
  ];
  const { data: projectRows, error: projectErr } = projectIds.length
    ? await admin.from('projects').select('id, name').in('id', projectIds)
    : { data: [] as { id: string; name: string }[], error: null };
  if (projectErr) throw projectErr;
  const projectNames = new Map(projectRows.map((p) => [p.id, p.name]));

  return data.map((row) => ({
    ...row,
    actorLogin: row.actor ? (logins.get(row.actor)?.login ?? null) : null,
    projectName: row.project_id ? (projectNames.get(row.project_id) ?? null) : null,
  }));
}

// A plain list of every distinct action already in the log, for the
// filter dropdown — derived from real data rather than a hardcoded
// enum, so it never drifts from whatever action names actually get
// written (audit_log.action has no DB-level enum constraint, by
// design — see database-rls.md's own history of actions added one
// ticket at a time).
export async function listDistinctAuditActions(): Promise<string[]> {
  const admin = createAdminClient();
  const { data, error } = await admin.from('audit_log').select('action');
  if (error) throw error;
  return [...new Set(data.map((r) => r.action))].sort();
}

const HEARTBEAT_ONLINE_AFTER_MS = 10 * 60 * 1000; // 2x the 5-minute interval
const HEARTBEAT_STALE_AFTER_MS = 30 * 60 * 1000; // 6x the interval

export async function listAgentHeartbeats(): Promise<HeartbeatWithLogin[]> {
  const admin = createAdminClient();
  const { data, error } = await admin
    .from('agent_heartbeats')
    .select('*')
    .order('last_seen_at', { ascending: false });
  if (error) throw error;

  const logins = await resolveLogins(data.map((h) => h.user_id));
  const now = Date.now();
  return data.map((h) => {
    const age = now - new Date(h.last_seen_at).getTime();
    const status: HeartbeatWithLogin['status'] =
      age <= HEARTBEAT_ONLINE_AFTER_MS
        ? 'online'
        : age <= HEARTBEAT_STALE_AFTER_MS
          ? 'stale'
          : 'offline';
    return { ...h, login: logins.get(h.user_id)?.login ?? null, status };
  });
}

export async function getRedactionConfig(): Promise<RedactionConfigRow> {
  const admin = createAdminClient();
  const { data, error } = await admin
    .from('redaction_config')
    .select('*')
    .eq('id', 'default')
    .maybeSingle();
  if (error) throw error;
  // The row is seeded by CO-301's own migration — it always exists,
  // but fall back rather than throw if it's somehow missing.
  return (
    data ?? {
      id: 'default',
      disabled_rule_ids: [],
      denylist_additions: [],
      pinned_version: null,
      updated_at: new Date(0).toISOString(),
    }
  );
}

const AI_COST_WINDOW_DAYS = 30;

// CO-504: the /admin/dashboard page. lagP50/95 and fleetErrorRate call
// the exact same SQL functions (lag_p50_seconds, lag_p95_seconds,
// fleet_upload_error_rate) check-observability-alerts itself uses to
// decide whether to fire — this ticket's own "the dashboard numbers
// match database queries" testing point, satisfied by construction
// rather than two independent implementations that could drift.
export async function getObservabilityDashboard(): Promise<ObservabilityDashboard> {
  const admin = createAdminClient();

  const [
    lagP50,
    lagP95,
    errorRate,
    summarizeP50,
    summarizeP95,
    summarizeFailureRate,
    storageTotals,
    projectRows,
    aiUsageRows,
  ] = await Promise.all([
    admin.rpc('lag_p50_seconds'),
    admin.rpc('lag_p95_seconds'),
    admin.rpc('fleet_upload_error_rate'),
    admin.rpc('summarize_job_p50_latency_ms'),
    admin.rpc('summarize_job_p95_latency_ms'),
    admin.rpc('summarize_job_failure_rate'),
    admin.rpc('project_storage_totals'),
    admin.from('projects').select('id, name'),
    admin
      .from('ai_usage')
      .select('cost_usd, created_at')
      .gte(
        'created_at',
        new Date(Date.now() - AI_COST_WINDOW_DAYS * 24 * 60 * 60 * 1000).toISOString(),
      ),
  ]);
  if (lagP50.error) throw lagP50.error;
  if (lagP95.error) throw lagP95.error;
  if (errorRate.error) throw errorRate.error;
  if (summarizeP50.error) throw summarizeP50.error;
  if (summarizeP95.error) throw summarizeP95.error;
  if (summarizeFailureRate.error) throw summarizeFailureRate.error;
  if (storageTotals.error) throw storageTotals.error;
  if (projectRows.error) throw projectRows.error;
  if (aiUsageRows.error) throw aiUsageRows.error;

  const projectNameById = new Map((projectRows.data ?? []).map((p) => [p.id, p.name]));
  const storageByProject = (
    (storageTotals.data ?? []) as { project_id: string; total_bytes: number }[]
  )
    .map((row) => ({
      projectId: row.project_id,
      projectName: projectNameById.get(row.project_id) ?? row.project_id,
      totalBytes: row.total_bytes,
    }))
    .sort((a, b) => b.totalBytes - a.totalBytes);

  const costByDate = new Map<string, number>();
  for (const row of aiUsageRows.data ?? []) {
    const date = row.created_at.slice(0, 10);
    costByDate.set(date, (costByDate.get(date) ?? 0) + Number(row.cost_usd));
  }
  const aiCostByDay = [...costByDate.entries()]
    .map(([date, costUsd]) => ({ date, costUsd }))
    .sort((a, b) => b.date.localeCompare(a.date));

  return {
    lagP50Seconds: lagP50.data as number | null,
    lagP95Seconds: lagP95.data as number | null,
    fleetErrorRate: errorRate.data as number | null,
    summarizeJobP50Ms: summarizeP50.data as number | null,
    summarizeJobP95Ms: summarizeP95.data as number | null,
    summarizeJobFailureRate: summarizeFailureRate.data as number | null,
    aiCostByDay,
    storageByProject,
  };
}
