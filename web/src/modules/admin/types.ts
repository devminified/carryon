import type { Database } from '@/shared/types/database.types';

export type ProjectRow = Database['public']['Tables']['projects']['Row'];
export type ProjectMemberRow = Database['public']['Tables']['project_members']['Row'];
export type NotificationSettingsRow =
  Database['public']['Tables']['project_notification_settings']['Row'];
export type AuditLogRow = Database['public']['Tables']['audit_log']['Row'];
export type AgentHeartbeatRow = Database['public']['Tables']['agent_heartbeats']['Row'];
export type RedactionConfigRow = Database['public']['Tables']['redaction_config']['Row'];

// A project row plus its (possibly absent) notification settings and
// GitHub-team mapping, shaped for the Projects page's one edit form —
// see data/queries.ts's listProjectsWithSettings.
export type ProjectWithSettings = ProjectRow & {
  slackWebhookUrl: string | null;
  notifyDigest: boolean;
};

// A project_members row plus the member's resolved GitHub login/email
// (auth.users metadata isn't reachable via RLS — same reason list-
// sessions' own Edge Function needs the service role).
export type MemberWithLogin = ProjectMemberRow & {
  login: string | null;
  email: string | null;
};

export type AuditLogEntry = AuditLogRow & {
  actorLogin: string | null;
  projectName: string | null;
};

// A heartbeat row plus the reporting developer's resolved login, and
// a derived liveness bucket the Agents page's badge reads directly —
// computed server-side (data/queries.ts) from last_seen_at compared
// against HEARTBEAT_STALE_AFTER_MS, not recomputed client-side, so a
// server-rendered page and a later revalidation agree on the same
// "as of" instant.
export type HeartbeatWithLogin = AgentHeartbeatRow & {
  login: string | null;
  status: 'online' | 'stale' | 'offline';
};

// CO-504: the /admin/dashboard page's own real numbers — lagP50/95
// and fleetErrorRate come straight from the same SQL functions
// check-observability-alerts itself calls (lag_p50_seconds,
// lag_p95_seconds, fleet_upload_error_rate), so the dashboard and the
// alert sweep can never silently disagree about what "right now"
// means. null is a real, distinct state from 0 (see each function's
// own doc comment) — "no data yet," not "0% error rate."
export type ObservabilityDashboard = {
  lagP50Seconds: number | null;
  lagP95Seconds: number | null;
  fleetErrorRate: number | null;
  aiCostByDay: { date: string; costUsd: number }[];
  storageByProject: { projectId: string; projectName: string; totalBytes: number }[];
};
