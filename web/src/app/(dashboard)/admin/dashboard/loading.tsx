import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { AI_ENABLED } from '@/shared/lib/ai-enabled';
import { Skeleton } from '@/shared/ui/skeleton';

// Matches observability-dashboard.tsx's own grid-of-cards + two small
// tables — the AI-cost/summary-job tiles and table only ever show at
// all when AI_ENABLED (a build-time constant, safe to check here
// synchronously, same as the real component does).
export default function Loading() {
  return (
    <AdminPageShell>
      <div className="mt-4 flex flex-col gap-6">
        <div className="grid grid-cols-3 gap-4">
          {Array.from({ length: AI_ENABLED ? 5 : 2 }).map((_, i) => (
            <div key={i} className="border-border rounded border p-4">
              <Skeleton className="h-3 w-32" />
              <Skeleton className="mt-2 h-6 w-20" />
            </div>
          ))}
        </div>

        {AI_ENABLED && (
          <div>
            <Skeleton className="mb-2 h-4 w-28" />
            <Skeleton className="h-24 w-full max-w-md" />
          </div>
        )}

        <div>
          <Skeleton className="mb-2 h-4 w-40" />
          <Skeleton className="h-24 w-full max-w-md" />
        </div>
      </div>
    </AdminPageShell>
  );
}
