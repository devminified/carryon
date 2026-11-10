// CO-504: small pure formatters for the observability dashboard, kept
// out of ui/observability-dashboard.tsx (layers.md) so they're
// directly unit-testable without rendering anything.

export function formatSeconds(seconds: number | null): string {
  if (seconds === null) return 'No data yet';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  return `${Math.round(seconds / 60)}m`;
}

export function formatRate(rate: number | null): string {
  if (rate === null) return 'No data yet';
  return `${(rate * 100).toFixed(1)}%`;
}

// CO-504: summarize_job_p50/p95_latency_ms are already milliseconds
// (duration_ms, from summarize_job_runs) — reuses formatSeconds'
// own thresholds once converted, rather than a second ad hoc scale.
export function formatMillis(ms: number | null): string {
  if (ms === null) return 'No data yet';
  return formatSeconds(ms / 1000);
}
