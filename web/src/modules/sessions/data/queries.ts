import 'server-only';
import { createClient } from '@/shared/lib/supabase/server';
import { createAdminClient } from '@/shared/lib/supabase/admin';
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
  if (filters.q) {
    query = query.textSearch('search_vector', filters.q, {
      type: 'websearch',
      config: 'english',
    });
  }
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
    lastActivityAt: r.last_activity_at,
    totalBytes: r.total_bytes,
    ownerLogin: logins.get(r.user_id) ?? null,
    pinned: r.pinned,
  }));

  const last = page[page.length - 1];
  const nextCursor =
    hasMore && last
      ? { pinned: last.pinned, lastActivityAt: last.last_activity_at, id: last.id }
      : null;

  return { items, nextCursor };
}
