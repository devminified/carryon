// Same shape as TranscriptView's own inline loading placeholder
// (useStream's progressive chunk loading, see that component) so
// there's no visual jump when this route-level fallback hands off to
// the real, mounted component's own fallback.
export function TranscriptSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="border-border bg-border/30 h-16 animate-pulse rounded border" />
      ))}
    </div>
  );
}
