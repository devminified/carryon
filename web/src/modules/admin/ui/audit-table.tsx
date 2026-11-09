import { formatRelativeTime } from '@/shared/lib/format/date';
import type { AuditLogEntry } from '../types';

export function AuditTable({ rows }: { rows: AuditLogEntry[] }) {
  if (rows.length === 0) {
    return <p className="text-muted-foreground mt-4 text-sm">No audit rows match these filters.</p>;
  }

  return (
    <table className="mt-4 w-full text-left text-sm">
      <thead>
        <tr className="border-border border-b text-xs font-medium">
          <th className="py-2">When</th>
          <th className="py-2">Action</th>
          <th className="py-2">Actor</th>
          <th className="py-2">Project</th>
          <th className="py-2">Reason</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.id} className="border-border border-b">
            <td className="py-2" title={row.at}>
              {formatRelativeTime(row.at)}
            </td>
            <td className="py-2">{row.action}</td>
            <td className="py-2">{row.actorLogin ?? row.actor ?? '—'}</td>
            <td className="py-2">{row.projectName ?? '—'}</td>
            <td className="text-muted-foreground py-2">{row.reason ?? ''}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
