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
