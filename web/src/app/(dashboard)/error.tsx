'use client';

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="flex flex-col items-start gap-3">
      <p className="text-destructive text-sm">
        Something went wrong{error.digest ? ` (${error.digest})` : ''}.
      </p>
      <button type="button" onClick={reset} className="text-muted-foreground text-sm underline">
        Try again
      </button>
    </div>
  );
}
