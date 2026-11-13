'use client';

import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table';

// CO-704: the shared wrapper .claude/rules/tables.md already
// documented but never built. Scoped to what session-table.tsx and
// members-table.tsx actually need today — columns, data, and an
// optional empty state — not the rules doc's original aspirational
// pagination/sorting/rowSelection signature. Neither table has a
// user-facing sort or filter control (session-table.tsx's own sort is
// server-driven plus a post-realtime-merge pinned-first re-sort, not
// something this wrapper needs to know about), and nothing in this
// app does row selection yet. Add those props when a real consumer
// needs them, not before (CLAUDE.md: no premature abstraction).
export function DataTable<TData>({
  columns,
  data,
  emptyState,
  getRowId,
}: {
  columns: ColumnDef<TData>[];
  data: TData[];
  emptyState?: React.ReactNode;
  // Without this, a row's id defaults to its array index — fine for a
  // static list, but wrong the moment `data` can reorder (session-
  // table.tsx's pinned-first re-sort after a pin/unpin). React then
  // keys TableRow by an index that now points at a different session,
  // so it remounts that row's whole subtree instead of reconciling it
  // in place — silently destroying any row-local state (PinButton's
  // own useOptimistic) along with it. Found via CO-704's own e2e
  // suite: the optimistic-pin-timing test started missing its 150ms
  // budget because the remount, not a real latency regression, was
  // what delayed the label flip.
  getRowId?: (row: TData) => string;
}) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    ...(getRowId && { getRowId }),
  });

  if (data.length === 0 && emptyState) {
    return <>{emptyState}</>;
  }

  return (
    <Table>
      <TableHeader>
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id}>
            {headerGroup.headers.map((header) => (
              <TableHead key={header.id} className={header.column.columnDef.meta?.headClassName}>
                {header.isPlaceholder
                  ? null
                  : flexRender(header.column.columnDef.header, header.getContext())}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id} className={cell.column.columnDef.meta?.cellClassName}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

declare module '@tanstack/react-table' {
  // Augmenting this interface has to match its original generic arity
  // even though neither param is used in the added fields.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData, TValue> {
    headClassName?: string;
    cellClassName?: string;
  }
}
