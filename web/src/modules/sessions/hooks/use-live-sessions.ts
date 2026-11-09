'use client';

import { useEffect, useRef, useState } from 'react';
import { createClient } from '@/shared/lib/supabase/client';
import type { SessionListFilters, SessionListItem, SessionRow } from '../types';

function rowMatchesFilters(row: SessionRow, filters: SessionListFilters): boolean {
  if (filters.status && row.status !== filters.status) return false;
  if (filters.outcome && row.outcome !== filters.outcome) return false;
  if (filters.branch && row.branch !== filters.branch) return false;
  if (filters.from && row.last_activity_at < filters.from) return false;
  if (filters.to && row.last_activity_at > filters.to) return false;
  // dev/q filters need a login lookup or full-text match this client
  // can't do locally — a realtime row for either stays excluded rather
  // than risk showing something the user explicitly filtered out.
  if (filters.dev || filters.q) return false;
  return true;
}

function toListItem(row: SessionRow, ownerLogin: string | null): SessionListItem {
  return {
    id: row.id,
    projectId: row.project_id,
    userId: row.user_id,
    title: row.title,
    summary: row.summary,
    branch: row.branch,
    status: row.status,
    outcome: row.outcome,
    lastActivityAt: row.last_activity_at,
    totalBytes: row.total_bytes,
    ownerLogin,
    pinned: row.pinned,
  };
}

// Same sort as listSessionsForProject's own ORDER BY (pinned desc,
// last_activity_at desc) — a realtime pin/unpin toggle (or any
// last_activity_at bump) must re-sort in place, not just patch the row
// where it already sits, or a newly-pinned row would stay wherever it
// happened to be instead of jumping to the top.
export function sortItems(items: SessionListItem[]): SessionListItem[] {
  return [...items].sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
    return b.lastActivityAt.localeCompare(a.lastActivityAt);
  });
}

// Subscribes to Supabase Realtime for one project's sessions (CO-402).
// RLS still gates what a subscriber actually receives (see the
// migration's own comment) — this only decides how to merge what
// arrives into the page the caller is currently looking at.
export function useLiveSessions(
  projectId: string,
  initialItems: SessionListItem[],
  filters: SessionListFilters,
  isFirstPage: boolean,
) {
  // Adjusting state when a prop changes (react.dev's own pattern for
  // this, not an effect) — a navigation to a new page/filter hands this
  // hook a new initialItems array, and realtime-merged local state
  // should reset to it rather than keep merging onto stale data.
  const [prevInitialItems, setPrevInitialItems] = useState(initialItems);
  const [items, setItems] = useState(initialItems);
  if (initialItems !== prevInitialItems) {
    setPrevInitialItems(initialItems);
    setItems(initialItems);
  }

  const [now, setNow] = useState(() => Date.now());
  const itemsRef = useRef(items);
  useEffect(() => {
    itemsRef.current = items;
  }, [items]);

  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), 15_000);
    return () => clearInterval(tick);
  }, []);

  useEffect(() => {
    const supabase = createClient();
    let channel: ReturnType<typeof supabase.channel> | null = null;
    let cancelled = false;

    // Explicit, awaited setAuth before subscribing — belt-and-suspenders
    // against a race between this effect and SupabaseClient's own
    // onAuthStateChange-driven realtime.setAuth() wiring, which fires
    // asynchronously off INITIAL_SESSION and isn't guaranteed to land
    // before a channel.subscribe() call right after mount.
    supabase.auth.getSession().then(async ({ data }) => {
      if (cancelled) return;
      if (data.session?.access_token) {
        await supabase.realtime.setAuth(data.session.access_token);
      }
      if (cancelled) return;

      channel = supabase
        .channel(`sessions:project:${projectId}`)
        .on(
          'postgres_changes',
          {
            event: '*',
            schema: 'public',
            table: 'sessions',
            filter: `project_id=eq.${projectId}`,
          },
          (payload) => {
            const row = payload.new as SessionRow | undefined;
            if (!row) return;

            const current = itemsRef.current;
            const existingIndex = current.findIndex((s) => s.id === row.id);

            if (existingIndex >= 0) {
              if (!rowMatchesFilters(row, filters)) {
                setItems(current.filter((s) => s.id !== row.id));
                return;
              }
              const next = [...current];
              next[existingIndex] = toListItem(row, next[existingIndex].ownerLogin);
              setItems(sortItems(next));
              return;
            }

            if (isFirstPage && rowMatchesFilters(row, filters)) {
              setItems(sortItems([toListItem(row, null), ...current]));
            }
          },
        )
        .subscribe();
    });

    return () => {
      cancelled = true;
      if (channel) void supabase.removeChannel(channel);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, isFirstPage]);

  return { items, now };
}
