'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { Pin } from 'lucide-react';
import type { ColumnDef } from '@tanstack/react-table';
import { useLiveSessions } from '../hooks/use-live-sessions';
import { LiveDot } from './live-dot';
import { OutcomeBadge } from './outcome-badge';
import { PinButton } from './pin-button';
import { ResummarizeButton } from './resummarize-button';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { formatBytes } from '@/shared/lib/format/bytes';
import { AI_ENABLED } from '@/shared/lib/ai-enabled';
import { DataTable } from '@/shared/ui/data-table';
import { LIVE_ACTIVITY_WINDOW_MS, type SessionListFilters, type SessionListItem } from '../types';

// CO-704: columns live here, not a separate session-table-columns.tsx
// — every cell needs projectId/isAdmin/now from this component's own
// closure (the pin/resummarize buttons, the live-window calculation,
// relative-time formatting), so splitting them out would just mean
// passing all three back in as a function anyway. tables.md's own
// example has no such closure-dependent cell, which is why it shows
// a plain top-level array instead.
function useSessionColumns(
  projectId: string,
  isAdmin: boolean,
  now: Date,
): ColumnDef<SessionListItem>[] {
  return useMemo(
    () => [
      {
        id: 'pinned',
        header: () => <span className="sr-only">Pinned</span>,
        meta: { headClassName: 'w-6' },
        cell: ({ row }) => {
          const s = row.original;
          return isAdmin ? (
            <PinButton sessionId={s.id} projectId={projectId} pinned={s.pinned} />
          ) : (
            s.pinned && (
              <Pin aria-label="Pinned session" className="text-foreground size-4 fill-current" />
            )
          );
        },
      },
      {
        id: 'session',
        header: 'Session',
        meta: { cellClassName: 'whitespace-normal' },
        cell: ({ row }) => {
          const s = row.original;
          return (
            <>
              <Link href={`/sessions/${s.id}`} className="text-foreground font-medium underline">
                {s.title ?? '(untitled session)'}
              </Link>
              {s.summary && (
                <p className="text-muted-foreground line-clamp-2 text-xs">{s.summary}</p>
              )}
              {/* CO-501: the passage search_sessions matched this session on —
                  only present on a semantic-search result, never the plain list. */}
              {s.matchingPassage && (
                <p className="bg-warning/10 text-warning mt-1 line-clamp-2 rounded px-1.5 py-0.5 text-xs">
                  “{s.matchingPassage}”
                </p>
              )}
              <div className="text-muted-foreground/70 flex items-center gap-1 text-xs">
                {s.summary && (
                  <span>
                    AI-generated
                    {s.summarizedAt && ` · ${formatRelativeTime(s.summarizedAt, now)}`}
                    {s.filesTouched.length > 0 && (
                      <span title={s.filesTouched.join('\n')}>
                        {' '}
                        · {s.filesTouched.length} file{s.filesTouched.length === 1 ? '' : 's'}
                      </span>
                    )}
                  </span>
                )}
                {isAdmin && AI_ENABLED && (
                  <>
                    {s.summary && <span>·</span>}
                    <ResummarizeButton sessionId={s.id} projectId={projectId} />
                  </>
                )}
              </div>
            </>
          );
        },
      },
      {
        id: 'developer',
        header: 'Developer',
        meta: { cellClassName: 'text-foreground' },
        cell: ({ row }) => row.original.ownerLogin ?? '—',
      },
      {
        id: 'branch',
        header: 'Branch',
        meta: { cellClassName: 'text-foreground' },
        cell: ({ row }) => row.original.branch ?? '—',
      },
      {
        id: 'status',
        header: 'Status',
        cell: ({ row }) => {
          const s = row.original;
          const live =
            s.status === 'live' &&
            now.getTime() - new Date(s.lastActivityAt).getTime() < LIVE_ACTIVITY_WINDOW_MS;
          return (
            <span className="text-foreground inline-flex items-center gap-1.5">
              <LiveDot live={live} />
              {s.status}
            </span>
          );
        },
      },
      {
        id: 'outcome',
        header: 'Outcome',
        cell: ({ row }) => (
          <OutcomeBadge outcome={row.original.outcome} blockedReason={row.original.blockedReason} />
        ),
      },
      {
        id: 'lastActivity',
        header: 'Last activity',
        meta: { cellClassName: 'text-foreground' },
        cell: ({ row }) => formatRelativeTime(row.original.lastActivityAt, now),
      },
      {
        id: 'size',
        header: 'Size',
        meta: { cellClassName: 'text-foreground' },
        cell: ({ row }) => formatBytes(row.original.totalBytes),
      },
    ],
    [projectId, isAdmin, now],
  );
}

export function SessionTable({
  projectId,
  initialItems,
  filters,
  isFirstPage,
  isAdmin,
  now: initialNow,
}: {
  projectId: string;
  initialItems: SessionListItem[];
  filters: SessionListFilters;
  isFirstPage: boolean;
  // CO-405: only an admin sees the pin toggle at all — RLS
  // (admins_can_update_sessions) is still the real enforcement if this
  // ever disagreed with the server's own view of the caller's role.
  isAdmin: boolean;
  // CO-601: server-computed timestamp, see use-live-sessions.ts's own
  // comment for why this can't be computed client-side in this
  // component without risking a hydration mismatch.
  now: string;
}) {
  const { items, now, realtimeStatus } = useLiveSessions(
    projectId,
    initialItems,
    filters,
    isFirstPage,
    initialNow,
  );
  const columns = useSessionColumns(projectId, isAdmin, now);

  // Invisible, unstyled marker — exists only so the e2e suite can wait
  // for a real Realtime subscription to actually be live before
  // triggering a change it expects to arrive without a reload, instead
  // of racing a fixed sleep against the channel's own WebSocket
  // handshake. Realtime has no event replay/backlog: a change
  // published before the channel reaches 'SUBSCRIBED' is simply never
  // delivered, not delivered late — no amount of waiting afterward
  // would help. Found the hard way as this exact page's own e2e test
  // flaking under CI (session-list.spec.ts's Realtime test).
  const statusMarker = <span data-realtime-status={realtimeStatus} hidden />;

  return (
    <>
      {statusMarker}
      <DataTable
        columns={columns}
        data={items}
        getRowId={(item) => item.id}
        emptyState={
          <p className="text-muted-foreground text-sm">No sessions match these filters.</p>
        }
      />
    </>
  );
}
