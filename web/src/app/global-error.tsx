'use client';

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="en">
      <body className="flex min-h-screen flex-col items-center justify-center gap-3">
        <p className="text-sm text-red-600">
          Something went wrong{error.digest ? ` (${error.digest})` : ''}.
        </p>
        <button type="button" onClick={reset} className="text-sm underline">
          Try again
        </button>
      </body>
    </html>
  );
}
