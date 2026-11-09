import type { SessionListFilters, SessionListItem } from '../types';

// The semantic search path (CO-501) can't express dev/status/outcome/
// branch/date filters inside the `search` Edge Function's own RPC —
// search_sessions only takes a query, an optional project, and a match
// count. Those filters apply here instead, as a narrowing over the
// (already small, matchCount-capped) ranked result set — same
// semantics as listSessionsForProject's own .eq()/.gte()/.lte() calls,
// just evaluated in JS instead of SQL. A plain pure function kept out
// of data/queries.ts (which imports 'server-only') so it can be unit
// tested directly.
export function matchesSessionListFilters(
  item: Pick<SessionListItem, 'ownerLogin' | 'status' | 'outcome' | 'branch' | 'lastActivityAt'>,
  filters: Omit<SessionListFilters, 'q'>,
): boolean {
  if (filters.dev && item.ownerLogin !== filters.dev) return false;
  if (filters.status && item.status !== filters.status) return false;
  if (filters.outcome && item.outcome !== filters.outcome) return false;
  if (filters.branch && item.branch !== filters.branch) return false;
  if (filters.from && item.lastActivityAt < filters.from) return false;
  if (filters.to && item.lastActivityAt > filters.to) return false;
  return true;
}
