import { formatRelativeTime } from '@/shared/lib/format/date';
import type { HeartbeatWithLogin } from '../types';

const STATUS_LABEL: Record<HeartbeatWithLogin['status'], string> = {
  online: 'Online',
  stale: 'Stale',
  offline: 'Offline',
};

const STATUS_CLASS: Record<HeartbeatWithLogin['status'], string> = {
  online: 'bg-green-100 text-green-800',
  stale: 'bg-yellow-100 text-yellow-800',
  offline: 'bg-red-100 text-red-800',
};

export function AgentsTable({ heartbeats }: { heartbeats: HeartbeatWithLogin[] }) {
  if (heartbeats.length === 0) {
    return <p className="text-muted-foreground mt-4 text-sm">No agent has checked in yet.</p>;
  }

  return (
    <table className="mt-4 w-full text-left text-sm">
      <thead>
        <tr className="border-border border-b text-xs font-medium">
          <th className="py-2">Developer</th>
          <th className="py-2">Machine</th>
          <th className="py-2">Version</th>
          <th className="py-2">Last check-in</th>
          <th className="py-2">Blocked sessions</th>
          <th className="py-2">Status</th>
        </tr>
      </thead>
      <tbody>
        {heartbeats.map((h) => (
          <tr key={`${h.user_id}-${h.machine_id}`} className="border-border border-b">
            <td className="py-2">{h.login ?? h.user_id}</td>
            <td className="py-2">{h.hostname}</td>
            <td className="py-2">{h.version}</td>
            <td className="py-2" title={h.last_seen_at}>
              {formatRelativeTime(h.last_seen_at)}
            </td>
            <td className="py-2">{h.blocked_sessions}</td>
            <td className="py-2">
              <span className={`rounded px-2 py-0.5 text-xs font-medium ${STATUS_CLASS[h.status]}`}>{STATUS_LABEL[h.status]}</span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
