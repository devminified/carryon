const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

// Intl.DateTimeFormat/RelativeTimeFormat until real recurrence/diff math
// is needed (directory default) — a flat list of thresholds is plenty
// for "last activity" display.
//
// `now` is required, not defaulted to `new Date()` — found the hard
// way (CO-601): a default evaluated fresh on every call meant a
// Client Component calling this bare renders once server-side (one
// real instant) and once more at hydration (a different real
// instant), producing a text-content hydration mismatch whenever
// real time elapsed between the two passes. Every call site must now
// pass an explicit `now`, sourced from useLiveNow (shared/hooks/use-
// live-now.ts) in a Client Component so the server and the first
// client render see the identical value, or from a single `new
// Date()` at the top of a Server Component's own render (safe there
// — it only ever renders once).
export function formatRelativeTime(iso: string, now: Date): string {
  const diffMs = new Date(iso).getTime() - now.getTime();
  const diffSec = Math.round(diffMs / 1000);
  const abs = Math.abs(diffSec);

  if (abs < 60) return rtf.format(diffSec, 'second');
  const diffMin = Math.round(diffSec / 60);
  if (Math.abs(diffMin) < 60) return rtf.format(diffMin, 'minute');
  const diffHour = Math.round(diffMin / 60);
  if (Math.abs(diffHour) < 24) return rtf.format(diffHour, 'hour');
  const diffDay = Math.round(diffHour / 24);
  return rtf.format(diffDay, 'day');
}
