import { Skeleton } from '@/shared/ui/skeleton';
import { FamilyTreeSkeleton } from './family-tree-skeleton';

const DL_LABELS = ['Developer', 'Branch', 'Status', 'Outcome', 'Last activity', 'Size'];

// Mirrors side-panel.tsx cell-for-cell: title/rename row, summary
// lines, the dl of 6 metadata rows, the fork-history section, and the
// resume-command button.
export function SidePanelSkeleton() {
  return (
    <aside className="border-border flex w-72 flex-shrink-0 flex-col gap-4 border-l pl-4">
      <div>
        <Skeleton className="h-5 w-40" />
        <Skeleton className="mt-2 h-4 w-full" />
        <Skeleton className="mt-1 h-4 w-3/4" />
      </div>

      <dl className="flex flex-col gap-3">
        {DL_LABELS.map((label) => (
          <div key={label} className="flex flex-col gap-0.5">
            <dt className="text-muted-foreground text-xs">{label}</dt>
            <dd>
              <Skeleton className="h-4 w-24" />
            </dd>
          </div>
        ))}
      </dl>

      <FamilyTreeSkeleton />

      <div>
        <Skeleton className="mb-1 h-3 w-32" />
        <Skeleton className="h-8 w-full" />
      </div>
    </aside>
  );
}
