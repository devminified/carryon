import { formatRelativeTime } from '@/shared/lib/format/date';
import { formatBytes } from '@/shared/lib/format/bytes';
import { Badge } from '@/shared/ui/badge';
import type { HeartbeatWithLogin } from '../types';

const STATUS_LABEL: Record<HeartbeatWithLogin['status'], string> = {
  online: 'Online',
  stale: 'Stale',
  offline: 'Offline',
};

// Badge is RSC-safe (no 'use client' in its generated source) — unlike
// Table, which is, so this Server Component keeps its plain <table>
// (see this ticket's own "no new client boundary" testing point) but
// still gets Badge for this one status indicator.
const STATUS_CLASS: Record<HeartbeatWithLogin['status'], string> = {
  online: 'bg-success/10 text-success dark:bg-success/20',
  stale: 'bg-warning/10 text-warning dark:bg-warning/20',
  offline: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
};

export function AgentsTable({ heartbeats }: { heartbeats: HeartbeatWithLogin[] }) {
  if (heartbeats.length === 0) {
    return <p className="text-muted-foreground mt-4 text-sm">No agent has checked in yet.</p>;
  }

  // A Server Component renders exactly once, server-side — computing
  // `now` here (rather than defaulting inside formatRelativeTime,
  // CO-601) is always safe, never hydrated against a second instant.
  const now = new Date();

  return (
    <table className="mt-4 w-full text-left text-sm">
      <thead>
        <tr className="border-border border-b text-xs font-medium">
          <th className="py-2">Developer</th>
          <th className="py-2">Machine</th>
          <th className="py-2">OS</th>
          <th className="py-2">Version</th>
          <th className="py-2">Last check-in</th>
          <th className="py-2">Last upload</th>
          <th className="py-2">Bytes behind</th>
          <th className="py-2">Blocked sessions</th>
          {/* CO-504: counts since the last heartbeat, not an all-time
              total — see internal/upload.Uploader.TakeCounts' own
              doc comment. */}
          <th className="py-2">Errors</th>
          <th className="py-2">Redactions</th>
          <th className="py-2">Status</th>
        </tr>
      </thead>
      <tbody>
        {heartbeats.map((h) => (
          <tr key={`${h.user_id}-${h.machine_id}`} className="border-border border-b">
            <td className="py-2">{h.login ?? h.user_id}</td>
            <td className="py-2">{h.hostname}</td>
            <td className="py-2">{h.os ?? '—'}</td>
            <td className="py-2">{h.version}</td>
            <td className="py-2" title={h.last_seen_at}>
              {formatRelativeTime(h.last_seen_at, now)}
            </td>
            <td className="py-2" title={h.last_upload_at ?? undefined}>
              {h.last_upload_at ? formatRelativeTime(h.last_upload_at, now) : '—'}
            </td>
            <td className="py-2">{formatBytes(h.bytes_behind)}</td>
            <td className="py-2">{h.blocked_sessions}</td>
            <td className="py-2">{h.upload_errors}</td>
            <td className="py-2">{h.redactions_applied}</td>
            <td className="py-2">
              <Badge className={STATUS_CLASS[h.status]}>{STATUS_LABEL[h.status]}</Badge>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
