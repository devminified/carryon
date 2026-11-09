'use client';

import Link from 'next/link';
import { useLiveSessions } from '../hooks/use-live-sessions';
import { LiveDot } from './live-dot';
import { OutcomeBadge } from './outcome-badge';
import { PinButton } from './pin-button';
import { ResummarizeButton } from './resummarize-button';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { formatBytes } from '@/shared/lib/format/bytes';
import { LIVE_ACTIVITY_WINDOW_MS, type SessionListFilters, type SessionListItem } from '../types';

export function SessionTable({
  projectId,
  initialItems,
  filters,
  isFirstPage,
  isAdmin,
}: {
  projectId: string;
  initialItems: SessionListItem[];
  filters: SessionListFilters;
  isFirstPage: boolean;
  // CO-405: only an admin sees the pin toggle at all — RLS
  // (admins_can_update_sessions) is still the real enforcement if this
  // ever disagreed with the server's own view of the caller's role.
  isAdmin: boolean;
}) {
  const { items, now } = useLiveSessions(projectId, initialItems, filters, isFirstPage);

  if (items.length === 0) {
    return <p className="text-muted-foreground text-sm">No sessions match these filters.</p>;
  }

  return (
    <table className="w-full table-auto text-left text-sm">
      <thead>
        <tr className="border-border text-muted-foreground border-b">
          <th className="w-6 py-2 pr-2" aria-label="Pinned" />
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
              <td className="py-2 pr-2">
                {isAdmin ? (
                  <PinButton sessionId={s.id} projectId={projectId} pinned={s.pinned} />
                ) : (
                  s.pinned && (
                    <span
                      aria-label="Pinned session"
                      title="Pinned session"
                      className="text-base leading-none"
                    >
                      📌
                    </span>
                  )
                )}
              </td>
              <td className="py-2 pr-4">
                <Link href={`/sessions/${s.id}`} className="text-foreground font-medium underline">
                  {s.title ?? '(untitled session)'}
                </Link>
                {s.summary && (
                  <p className="text-muted-foreground line-clamp-2 text-xs">{s.summary}</p>
                )}
                {/* CO-501: the passage search_sessions matched this session on —
                    only present on a semantic-search result, never the plain list. */}
                {s.matchingPassage && (
                  <p className="mt-1 line-clamp-2 rounded bg-yellow-100 px-1.5 py-0.5 text-xs text-yellow-900">
                    “{s.matchingPassage}”
                  </p>
                )}
                <div className="text-muted-foreground/70 flex items-center gap-1 text-xs">
                  {s.summary && (
                    <span>
                      AI-generated
                      {s.summarizedAt && ` · ${formatRelativeTime(s.summarizedAt, new Date(now))}`}
                      {s.filesTouched.length > 0 && (
                        <span title={s.filesTouched.join('\n')}>
                          {' '}
                          · {s.filesTouched.length} file{s.filesTouched.length === 1 ? '' : 's'}
                        </span>
                      )}
                    </span>
                  )}
                  {isAdmin && (
                    <>
                      {s.summary && <span>·</span>}
                      <ResummarizeButton sessionId={s.id} projectId={projectId} />
                    </>
                  )}
                </div>
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
                <OutcomeBadge outcome={s.outcome} blockedReason={s.blockedReason} />
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
