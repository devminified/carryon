# Tables (`web/`)

Follow the directory default: a plain `<table>` is fine until a screen genuinely needs sort/filter/multi-select — then reach for the shared TanStack Table v8 wrapper. Don't add the wrapper pre-emptively to a table that's just a static list.

## Shared wrapper (once needed)

`web/src/shared/ui/data-table.tsx` exports a generic `<DataTable<TData>>` built on `useReactTable`. Accepts `columns`, `data`, optional server-side `pagination`/`sorting`, `rowSelection`, `isLoading`, `emptyState`. Modules never hand-roll their own table markup once they've adopted it.

## Column definition pattern

```tsx
// modules/sessions/ui/session-table-columns.tsx
'use client';
import type { ColumnDef } from '@tanstack/react-table';
import type { SessionRow } from '../types';
import { formatRelativeTime } from '@/shared/lib/format/date';

export const sessionColumns: ColumnDef<SessionRow>[] = [
  { accessorKey: 'title', header: 'Session' },
  { accessorKey: 'branch', header: 'Branch' },
  {
    accessorKey: 'last_activity_at',
    header: 'Last activity',
    cell: ({ row }) => formatRelativeTime(row.original.last_activity_at),
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => <StatusBadge status={row.original.status} />,
  },
];
```

## Server-side pagination

For tables that can genuinely grow large — the `audit_log` view on an admin page, or a project's full session history — use server-side pagination: the hook accepts `{ pageIndex, pageSize, sortBy }` and the data layer translates that into Supabase's `.range()`/`.order()`. The default session list for a small team doesn't need this on day one; add it when a project's session count makes client-side pagination slow, not before.
