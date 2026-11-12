import { Skeleton } from '@/shared/ui/skeleton';

// FamilyTree itself renders null for the common case (a session with
// no real fork relationship, see that component's own "<= 1" check) —
// this fallback only shows for the brief window while get-session-
// family (CO-608's own independently-streamed Suspense boundary, see
// family-tree-section.tsx) is still in flight, so it stays small and
// non-committal rather than guessing how many fork rows will appear.
export function FamilyTreeSkeleton() {
  return (
    <div>
      <Skeleton className="mb-1.5 h-3 w-20" />
      <div className="flex flex-col gap-1.5">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-4 w-32" />
      </div>
    </div>
  );
}
