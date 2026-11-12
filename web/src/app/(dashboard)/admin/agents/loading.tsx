import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { Skeleton } from '@/shared/ui/skeleton';

const HEADERS = [
  'Developer',
  'Machine',
  'OS',
  'Version',
  'Last check-in',
  'Last upload',
  'Bytes behind',
  'Blocked sessions',
  'Errors',
  'Redactions',
  'Status',
];

// Matches agents-table.tsx's own 11-column plain <table> (Server
// Component, no new client boundary — CO-605's own rule).
export default function Loading() {
  return (
    <AdminPageShell>
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
          {Array.from({ length: 6 }).map((_, i) => (
            <tr key={i} className="border-border border-b">
              {HEADERS.map((h) => (
                <td key={h} className="py-2">
                  {h === 'Status' ? (
                    <Skeleton className="h-5 w-16 rounded-full" />
                  ) : (
                    <Skeleton className="h-4 w-16" />
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </AdminPageShell>
  );
}
