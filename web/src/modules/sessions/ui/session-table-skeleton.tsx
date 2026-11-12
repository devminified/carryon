import { Skeleton } from '@/shared/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';

// Mirrors session-table.tsx's own column set and row shape cell-for-
// cell (loading.md's own rule), so swapping this out for the real
// table never shifts layout.
const HEADERS = ['Session', 'Developer', 'Branch', 'Status', 'Outcome', 'Last activity', 'Size'];

export function SessionTableSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-6" aria-label="Pinned" />
          {HEADERS.map((h) => (
            <TableHead key={h}>{h}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {Array.from({ length: rows }).map((_, i) => (
          <TableRow key={i}>
            <TableCell>
              <Skeleton className="size-4" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-48" />
              <Skeleton className="mt-1.5 h-3 w-32" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-20" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-24" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-16" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-5 w-16 rounded-full" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-20" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-12" />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
