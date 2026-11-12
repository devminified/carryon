export function LiveDot({ live }: { live: boolean }) {
  if (!live) return null;
  return (
    <span
      className="bg-live inline-block h-2 w-2 rounded-full"
      role="status"
      aria-label="live"
      title="Active in the last 5 minutes"
    />
  );
}
