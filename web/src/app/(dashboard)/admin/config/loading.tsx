import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { Skeleton } from '@/shared/ui/skeleton';

// Matches redaction-config-form.tsx: two labeled textareas, one
// labeled input, then the save button.
export default function Loading() {
  return (
    <AdminPageShell>
      <div className="max-w-2xl space-y-4">
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-48" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-56" />
          <Skeleton className="h-24 w-full" />
        </div>
        <div className="flex flex-col gap-1">
          <Skeleton className="h-4 w-36" />
          <Skeleton className="h-3 w-64" />
          <Skeleton className="h-9 w-48" />
        </div>
        <Skeleton className="h-9 w-32" />
      </div>
    </AdminPageShell>
  );
}
