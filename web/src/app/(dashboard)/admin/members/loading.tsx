import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { Skeleton } from '@/shared/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';

const HEADERS = ['Member', 'Role', 'Source', ''];

// Matches admin/members/page.tsx's own shape once a project is
// selected — the project picker, then members-table.tsx's columns,
// then add-member-form.tsx's own row of fields.
export default function Loading() {
  return (
    <AdminPageShell>
      <Skeleton className="h-8 w-48" />
      <Table className="mt-4">
        <TableHeader>
          <TableRow>
            {HEADERS.map((h, i) => (
              <TableHead key={i}>{h}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {Array.from({ length: 4 }).map((_, i) => (
            <TableRow key={i}>
              <TableCell>
                <Skeleton className="h-4 w-32" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-8 w-28" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-3 w-12" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-4 w-14" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <div className="border-border mt-4 flex flex-wrap items-end gap-3 rounded border p-3">
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-10" />
          <Skeleton className="h-9 w-48" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-8" />
          <Skeleton className="h-8 w-24" />
        </div>
        <Skeleton className="h-9 w-36" />
      </div>
    </AdminPageShell>
  );
}
