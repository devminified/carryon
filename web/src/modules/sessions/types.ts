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
  title: string | null;
  summary: string | null;
  branch: string | null;
  status: string;
  outcome: string | null;
  lastActivityAt: string;
  totalBytes: number;
  ownerLogin: string | null;
};

export type SessionListCursor = { lastActivityAt: string; id: string };

export type SessionListFilters = {
  dev?: string;
  status?: SessionStatus;
  outcome?: SessionOutcome;
  branch?: string;
  from?: string;
  to?: string;
  q?: string;
};
