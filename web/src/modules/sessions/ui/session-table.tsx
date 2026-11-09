'use client';

import Link from 'next/link';
import { useLiveSessions } from '../hooks/use-live-sessions';
import { LiveDot } from './live-dot';
import { OutcomeBadge } from './outcome-badge';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { formatBytes } from '@/shared/lib/format/bytes';
import { LIVE_ACTIVITY_WINDOW_MS, type SessionListFilters, type SessionListItem } from '../types';

export function SessionTable({
  projectId,
  initialItems,
  filters,
  isFirstPage,
}: {
  projectId: string;
  initialItems: SessionListItem[];
  filters: SessionListFilters;
  isFirstPage: boolean;
}) {
  const { items, now } = useLiveSessions(projectId, initialItems, filters, isFirstPage);

  if (items.length === 0) {
    return <p className="text-muted-foreground text-sm">No sessions match these filters.</p>;
  }

  return (
    <table className="w-full table-auto text-left text-sm">
      <thead>
        <tr className="border-border text-muted-foreground border-b">
          <th className="py-2 pr-4">Session</th>
          <th className="py-2 pr-4">Developer</th>
          <th className="py-2 pr-4">Branch</th>
          <th className="py-2 pr-4">Status</th>
          <th className="py-2 pr-4">Outcome</th>
          <th className="py-2 pr-4">Last activity</th>
          <th className="py-2 pr-4">Size</th>
        </tr>
      </thead>
      <tbody>
        {items.map((s) => {
          const live =
            s.status === 'live' &&
            now - new Date(s.lastActivityAt).getTime() < LIVE_ACTIVITY_WINDOW_MS;
          return (
            <tr key={s.id} className="border-border border-b">
              <td className="py-2 pr-4">
                <Link href={`/sessions/${s.id}`} className="text-foreground font-medium underline">
                  {s.title ?? '(untitled session)'}
                </Link>
                {s.summary && (
                  <p className="text-muted-foreground line-clamp-2 text-xs">{s.summary}</p>
                )}
              </td>
              <td className="text-foreground py-2 pr-4">{s.ownerLogin ?? '—'}</td>
              <td className="text-foreground py-2 pr-4">{s.branch ?? '—'}</td>
              <td className="py-2 pr-4">
                <span className="text-foreground inline-flex items-center gap-1.5">
                  <LiveDot live={live} />
                  {s.status}
                </span>
              </td>
              <td className="py-2 pr-4">
                <OutcomeBadge outcome={s.outcome} />
              </td>
              <td className="text-foreground py-2 pr-4">
                {formatRelativeTime(s.lastActivityAt, new Date(now))}
              </td>
              <td className="text-foreground py-2 pr-4">{formatBytes(s.totalBytes)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
