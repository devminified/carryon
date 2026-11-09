export function LiveDot({ live }: { live: boolean }) {
  if (!live) return null;
  return (
    <span
      className="inline-block h-2 w-2 rounded-full bg-green-500"
      role="status"
      aria-label="live"
      title="Active in the last 5 minutes"
    />
  );
}
