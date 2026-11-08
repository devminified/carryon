import { Skeleton } from '@/shared/ui/skeleton';

export default function Loading() {
  return (
    <div className="flex flex-col gap-3">
      <Skeleton className="h-5 w-48" />
      <Skeleton className="h-5 w-64" />
      <Skeleton className="h-5 w-40" />
    </div>
  );
}
