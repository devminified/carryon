import type { Database } from '@/shared/types/database.types';

export type SessionRow = Database['public']['Tables']['sessions']['Row'];
export type SessionStatus = 'live' | 'ended';
export type SessionOutcome = 'done' | 'in_progress' | 'blocked';

export const SESSION_STATUSES: readonly SessionStatus[] = ['live', 'ended'];
export const SESSION_OUTCOMES: readonly SessionOutcome[] = ['done', 'in_progress', 'blocked'];

// "live" within this window gets the live dot (CO-402's own wording).
export const LIVE_ACTIVITY_WINDOW_MS = 5 * 60 * 1000;

export type SessionListItem = {
  id: string;
  projectId: string;
  userId: string;
  title: string | null;
  summary: string | null;
  branch: string | null;
  status: string;
  outcome: string | null;
  lastActivityAt: string;
  totalBytes: number;
  ownerLogin: string | null;
  // CO-405
  pinned: boolean;
};

// pinned leads the sort (pinned desc, last_activity_at desc, id desc,
// see listSessionsForProject) — the cursor carries all three so a
// "Load more" page can resume exactly where the previous one ended,
// including across the pinned-to-unpinned boundary.
export type SessionListCursor = { pinned: boolean; lastActivityAt: string; id: string };

export type SessionListFilters = {
  dev?: string;
  status?: SessionStatus;
  outcome?: SessionOutcome;
  branch?: string;
  from?: string;
  to?: string;
  q?: string;
};
