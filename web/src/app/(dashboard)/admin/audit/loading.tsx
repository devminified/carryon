import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { Skeleton } from '@/shared/ui/skeleton';

const HEADERS = ['When', 'Action', 'Actor', 'Project', 'Reason'];

// Matches admin/audit/page.tsx: audit-filters.tsx's own row of 5
// fields + export link, then audit-table.tsx's plain <table> (a
// Server Component, no new client boundary — same reasoning this
// skeleton stays a plain <table> too, matching CO-605's own rule).
export default function Loading() {
  return (
    <AdminPageShell>
      <div className="border-border mb-4 flex flex-wrap items-end gap-3 border-b pb-4">
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-9 w-40" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-8 w-28" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-8 w-28" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-10" />
          <Skeleton className="h-9 w-28" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-3 w-10" />
          <Skeleton className="h-9 w-28" />
        </div>
        <Skeleton className="h-9 w-28" />
      </div>
      <table className="mt-4 w-full text-left text-sm">
        <thead>
          <tr className="border-border border-b text-xs font-medium">
            {HEADERS.map((h) => (
              <th key={h} className="py-2">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: 8 }).map((_, i) => (
            <tr key={i} className="border-border border-b">
              {HEADERS.map((h) => (
                <td key={h} className="py-2">
                  <Skeleton className="h-4 w-20" />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </AdminPageShell>
  );
}
