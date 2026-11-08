export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`bg-border animate-pulse rounded ${className}`} aria-hidden="true" />;
}
