import 'server-only';
import { createClient } from '@/shared/lib/supabase/server';
import { createAdminClient } from '@/shared/lib/supabase/admin';
import { callEdgeFunction } from '@/shared/lib/edge-function';
import { matchesSessionListFilters } from '../lib/search-filters';
import type { SessionListCursor, SessionListFilters, SessionListItem, SessionRow } from '../types';

export const SESSION_PAGE_SIZE = 50;

// auth.users' metadata isn't reachable via PostgREST/RLS (same reason
// list-sessions' own Edge Function needs the service role, see its doc
// comment) — the one lookup here that isn't RLS-scoped. Resolves in
// parallel, bounded by however many *unique* owners are on one page
// (at most SESSION_PAGE_SIZE + 1), and degrades to a null login on a
// lookup failure rather than failing the whole list — this is display
// metadata, not an access decision.
async function resolveLogins(userIds: string[]): Promise<Map<string, string | null>> {
  const admin = createAdminClient();
  const unique = [...new Set(userIds)];
  const entries = await Promise.all(
    unique.map(async (id) => {
      const { data, error } = await admin.auth.admin.getUserById(id);
      if (error || !data.user) {
        console.error('resolveLogins: lookup failed', id, error?.code);
        return [id, null] as const;
      }
      return [id, (data.user.user_metadata?.user_name as string | undefined) ?? null] as const;
    }),
  );
  return new Map(entries);
}

export async function listDevelopersForProject(projectId: string): Promise<string[]> {
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('project_members')
    .select('user_id')
    .eq('project_id', projectId);
  if (error) throw error;

  const logins = await resolveLogins((data ?? []).map((r) => r.user_id));
  return [...logins.values()].filter((l): l is string => l !== null).sort();
}

export async function listBranchesForProject(projectId: string): Promise<string[]> {
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('sessions')
    .select('branch')
    .eq('project_id', projectId)
    .not('branch', 'is', null)
    .limit(500);
  if (error) throw error;

  return [...new Set((data ?? []).map((r) => r.branch as string))].sort();
}

export async function listSessionsForProject(
  projectId: string,
  filters: SessionListFilters,
  cursor?: SessionListCursor,
): Promise<{ items: SessionListItem[]; nextCursor: SessionListCursor | null }> {
  const supabase = await createClient();

  let devUserId: string | null = null;
  if (filters.dev) {
    const developers = await listDevelopersForProject(projectId);
    if (!developers.includes(filters.dev)) {
      return { items: [], nextCursor: null };
    }
    // listDevelopersForProject already resolved every member's login —
    // resolve this one specific login back to an id so the filter runs
    // as real SQL (.eq on user_id), not a post-fetch JS filter.
    const { data: members } = await supabase
      .from('project_members')
      .select('user_id')
      .eq('project_id', projectId);
    const logins = await resolveLogins((members ?? []).map((m) => m.user_id));
    devUserId = [...logins.entries()].find(([, login]) => login === filters.dev)?.[0] ?? null;
    if (!devUserId) {
      return { items: [], nextCursor: null };
    }
  }

  // CO-405: pinned leads the sort. `.lt`/`.eq` on a boolean column
  // works the same way Postgres orders booleans (false < true) — the
  // cursor's three-level OR below is the same keyset-pagination
  // pattern CO-402 already used for (last_activity_at, id), just
  // extended one level for the new leading column.
  let query = supabase
    .from('sessions')
    .select('*')
    .eq('project_id', projectId)
    .order('pinned', { ascending: false })
    .order('last_activity_at', { ascending: false })
    .order('id', { ascending: false })
    .limit(SESSION_PAGE_SIZE + 1);

  if (devUserId) query = query.eq('user_id', devUserId);
  if (filters.status) query = query.eq('status', filters.status);
  if (filters.outcome) query = query.eq('outcome', filters.outcome);
  if (filters.branch) query = query.eq('branch', filters.branch);
  if (filters.from) query = query.gte('last_activity_at', filters.from);
  if (filters.to) query = query.lte('last_activity_at', filters.to);
  // CO-501: a `q` filter is handled entirely by searchSessionsForProject
  // below (the project page routes to it instead of calling this
  // function at all once a query is present) — plain full-text search
  // over search_vector is superseded by hybrid semantic search, not
  // run alongside it.
  if (cursor) {
    query = query.or(
      `pinned.lt.${cursor.pinned},` +
        `and(pinned.eq.${cursor.pinned},last_activity_at.lt.${cursor.lastActivityAt}),` +
        `and(pinned.eq.${cursor.pinned},last_activity_at.eq.${cursor.lastActivityAt},id.lt.${cursor.id})`,
    );
  }

  const { data, error } = await query;
  if (error) throw error;

  const rows: SessionRow[] = data ?? [];
  const hasMore = rows.length > SESSION_PAGE_SIZE;
  const page = hasMore ? rows.slice(0, SESSION_PAGE_SIZE) : rows;

  const logins = await resolveLogins(page.map((r) => r.user_id));

  const items: SessionListItem[] = page.map((r) => ({
    id: r.id,
    projectId: r.project_id,
    userId: r.user_id,
    title: r.title,
    summary: r.summary,
    branch: r.branch,
    status: r.status,
    outcome: r.outcome,
    blockedReason: r.blocked_reason,
    lastActivityAt: r.last_activity_at,
    totalBytes: r.total_bytes,
    ownerLogin: logins.get(r.user_id) ?? null,
    pinned: r.pinned,
    filesTouched: r.files_touched ?? [],
    summarizedAt: r.summarized_at,
  }));

  const last = page[page.length - 1];
  const nextCursor =
    hasMore && last
      ? { pinned: last.pinned, lastActivityAt: last.last_activity_at, id: last.id }
      : null;

  return { items, nextCursor };
}

interface SearchResponse {
  results: {
    sessionId: string;
    score: number;
    matchingPassage: string | null;
  }[];
}

// CO-501: the project page's search box, once a query is present —
// calls the `search` Edge Function (embeds the query, ranks sessions
// via the hybrid search_sessions SQL function, RLS-scoped by the
// caller's own JWT inside that function) and resolves the ranked IDs
// back into full SessionListItems through this app's own RLS-scoped
// client, the same mapping listSessionsForProject uses — so the table
// UI never has to know whether a row came from the plain list or
// search. No pagination: results are capped at one bounded page
// (SEARCH_MATCH_COUNT), ranked by relevance rather than recency, so a
// "Load more" cursor wouldn't mean anything here.
const SEARCH_MATCH_COUNT = 50;

export async function searchSessionsForProject(
  projectId: string,
  filters: SessionListFilters & { q: string },
): Promise<SessionListItem[]> {
  const supabase = await createClient();

  const { results } = await callEdgeFunction<SearchResponse>('search', {
    query: filters.q,
    projectId,
    matchCount: SEARCH_MATCH_COUNT,
  });
  if (results.length === 0) return [];

  const { data, error } = await supabase
    .from('sessions')
    .select('*')
    .in(
      'id',
      results.map((r) => r.sessionId),
    );
  if (error) throw error;

  const rows = (data ?? []) as SessionRow[];
  const rowById = new Map(rows.map((r) => [r.id, r]));
  const logins = await resolveLogins(rows.map((r) => r.user_id));

  return results
    .map((r) => {
      const row = rowById.get(r.sessionId);
      // search_sessions already scopes to what the caller can see via
      // RLS — a missing row here would mean it was deleted between the
      // two calls, not an access decision to make twice.
      if (!row) return null;
      const item: SessionListItem = {
        id: row.id,
        projectId: row.project_id,
        userId: row.user_id,
        title: row.title,
        summary: row.summary,
        branch: row.branch,
        status: row.status,
        outcome: row.outcome,
        blockedReason: row.blocked_reason,
        lastActivityAt: row.last_activity_at,
        totalBytes: row.total_bytes,
        ownerLogin: logins.get(row.user_id) ?? null,
        pinned: row.pinned,
        filesTouched: row.files_touched ?? [],
        summarizedAt: row.summarized_at,
        score: r.score,
        matchingPassage: r.matchingPassage,
      };
      return item;
    })
    .filter(
      (item): item is SessionListItem => item !== null && matchesSessionListFilters(item, filters),
    );
}
