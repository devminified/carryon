import { SessionTableSkeleton } from '@/modules/sessions/ui/session-table-skeleton';
import { Skeleton } from '@/shared/ui/skeleton';

// Mirrors ProjectPage's own shape: a project-name heading, the filter
// bar (session-filters.tsx has 8 controls: q, file, dev, status,
// outcome, branch, from, to), then the session table.
export default function Loading() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-6 w-48" />
      <div className="border-border flex flex-wrap items-center gap-2 border-b pb-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-8 w-32" />
        ))}
      </div>
      <SessionTableSkeleton />
    </div>
  );
}
