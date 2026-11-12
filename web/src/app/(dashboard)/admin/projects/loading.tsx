import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { Skeleton } from '@/shared/ui/skeleton';

// AdminPageShell itself has no data dependency (just the static
// "Admin" heading + nav), so it renders for real here too — only the
// data-dependent body (RegisterProjectForm + the registered-project
// list) is skeleton, matching admin/projects/page.tsx.
export default function Loading() {
  return (
    <AdminPageShell>
      <div className="border-border mb-6 flex flex-wrap items-end gap-3 rounded border p-4">
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-10" />
          <Skeleton className="h-9 w-40" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-36" />
          <Skeleton className="h-9 w-64" />
        </div>
        <Skeleton className="h-9 w-36" />
      </div>
      <ul className="space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <li key={i} className="border-border rounded border">
            <div className="flex items-center justify-between px-4 py-3">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-56" />
            </div>
          </li>
        ))}
      </ul>
    </AdminPageShell>
  );
}
