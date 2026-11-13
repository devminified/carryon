# Tables (`web/`)

Follow the directory default: a plain `<table>` is fine until a screen genuinely needs sort/filter/multi-select — then reach for the shared TanStack Table v8 wrapper. Don't add the wrapper pre-emptively to a table that's just a static list.

**Not every table in this app uses it.** `audit-table.tsx` and `agents-table.tsx` stay plain, hand-rolled Server Components by deliberate design (`agents-table.tsx`'s own comment) — `useReactTable` is client-only, and adopting it there would introduce a new client boundary neither table needs, since neither has sort/filter/multi-select today. `session-table.tsx` and `members-table.tsx` (CO-704/CO-705) adopted it because they're already Client Components with real row-level interactivity (pin/resummarize/role-change/remove).

**Pin `@tanstack/react-table` to the 8.x line, not the latest tag.** A `pnpm add @tanstack/react-table` with no version pin resolved to `9.x` during CO-704 — a from-scratch API rewrite (no `useReactTable`, no `getCoreRowModel`, a completely different "features" architecture) with nothing resembling the pattern this file documents. Install `@tanstack/react-table@8` explicitly.

## Shared wrapper (as actually built, CO-704)

`web/src/shared/ui/data-table.tsx` exports a generic `<DataTable<TData>>` built on `useReactTable`. Scoped to what its two real consumers actually need — **`columns`, `data`, and an optional `emptyState`** — not the originally-documented `pagination`/`sorting`/`rowSelection`/`isLoading` props: neither consumer has a user-facing sort or filter control (`session-table.tsx`'s own sort is server-driven, plus a post-realtime-merge pinned-first re-sort the table itself never needs to know about), pagination is handled above the table (a server-side "Load more" link), and nothing in this app does row selection yet. Add those props when a real third consumer actually needs one, not speculatively (CLAUDE.md: no premature abstraction).

A column's `meta.headClassName`/`meta.cellClassName` carry per-column styling through to the underlying `<TableHead>`/`<TableCell>` — DataTable doesn't forward arbitrary props onto those elements beyond that, by design.

**Always pass `getRowId` once a table's `data` can ever reorder.** `useReactTable`'s default row id is the row's array index, not anything about the row itself. `session-table.tsx`'s pinned-first re-sort after a pin/unpin changes a session's index, so without `getRowId` React's `key={row.id}` silently points at a different session after a re-sort — it remounts that row's whole subtree instead of reconciling it in place, destroying any row-local state (`PinButton`'s own `useOptimistic`) along with it. Found the hard way: CO-704's own optimistic-pin-timing e2e test (`rename-and-pin.spec.ts`) started missing its 150ms budget — not a real latency regression, but the remount discarding the optimistic flip and waiting on the real round trip instead. `DataTable` accepts an optional `getRowId: (row: TData) => string`; pass it (`getRowId={(item) => item.id}`) for any table whose data can reorder — a static list with no reordering can skip it.

## Column definition pattern

When a column's cells need to close over component state (the current user's role, a live-ticking `now`, a callback) — true for both of this app's real tables so far — define the columns *inside* the component via `useMemo`, not as a module-level constant in a separate file. A `session-table-columns.tsx` file only makes sense for columns with no such closure, which this app doesn't have yet:

```tsx
// inside session-table.tsx itself, not a separate file — see that
// file's own comment for why
function useSessionColumns(projectId: string, isAdmin: boolean, now: Date): ColumnDef<SessionListItem>[] {
  return useMemo(
    () => [
      { id: 'branch', header: 'Branch', cell: ({ row }) => row.original.branch ?? '—' },
      {
        id: 'lastActivity',
        header: 'Last activity',
        cell: ({ row }) => formatRelativeTime(row.original.lastActivityAt, now),
      },
      // ...
    ],
    [projectId, isAdmin, now],
  );
}
```

A column with no non-text content (icons, buttons, badges) still needs real header content for assistive tech even when it's visually empty — the pin column's header is `<span className="sr-only">Pinned</span>`, not a bare `<TableHead aria-label="Pinned" />`, since DataTable renders every header through `flexRender(column.columnDef.header, ...)` rather than letting a column attach arbitrary attributes to the `<th>` itself.

## Server-side pagination

For tables that can genuinely grow large — the `audit_log` view on an admin page, or a project's full session history — use server-side pagination: the hook accepts `{ pageIndex, pageSize, sortBy }` and the data layer translates that into Supabase's `.range()`/`.order()`. The default session list for a small team doesn't need this on day one; add it when a project's session count makes client-side pagination slow, not before.
