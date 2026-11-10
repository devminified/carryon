import { formatBytes } from '@/shared/lib/format/bytes';
import { formatMillis, formatRate, formatSeconds } from '../lib/format-observability';
import type { ObservabilityDashboard } from '../types';

// A plain numbers-and-tables page, no charting library — CLAUDE.md's
// own "no speculative scale," applied to this admin page's own UI:
// the fleet is small enough today that a handful of real numbers
// read at a glance, same reasoning agents-table.tsx's own plain
// <table> already follows.
export function ObservabilityDashboardView({ dashboard }: { dashboard: ObservabilityDashboard }) {
  return (
    <div className="mt-4 flex flex-col gap-6">
      <div className="grid grid-cols-3 gap-4">
        <div className="border-border rounded border p-4">
          <div className="text-muted-foreground text-xs">Cloud lag (p50 / p95)</div>
          <div className="text-foreground mt-1 text-lg font-semibold">
            {formatSeconds(dashboard.lagP50Seconds)} / {formatSeconds(dashboard.lagP95Seconds)}
          </div>
        </div>
        <div className="border-border rounded border p-4">
          <div className="text-muted-foreground text-xs">Fleet upload error rate</div>
          <div className="text-foreground mt-1 text-lg font-semibold">
            {formatRate(dashboard.fleetErrorRate)}
          </div>
        </div>
        <div className="border-border rounded border p-4">
          <div className="text-muted-foreground text-xs">
            AI cost, most recent day with usage
          </div>
          <div className="text-foreground mt-1 text-lg font-semibold">
            ${(dashboard.aiCostByDay[0]?.costUsd ?? 0).toFixed(2)}
          </div>
        </div>
        <div className="border-border rounded border p-4">
          <div className="text-muted-foreground text-xs">Summary job latency (p50 / p95)</div>
          <div className="text-foreground mt-1 text-lg font-semibold">
            {formatMillis(dashboard.summarizeJobP50Ms)} / {formatMillis(dashboard.summarizeJobP95Ms)}
          </div>
        </div>
        <div className="border-border rounded border p-4">
          <div className="text-muted-foreground text-xs">Summary job failure rate, last 24h</div>
          <div className="text-foreground mt-1 text-lg font-semibold">
            {formatRate(dashboard.summarizeJobFailureRate)}
          </div>
        </div>
      </div>

      <div>
        <h2 className="text-foreground mb-2 text-sm font-semibold">AI cost per day</h2>
        {dashboard.aiCostByDay.length === 0 ? (
          <p className="text-muted-foreground text-sm">No AI usage recorded yet.</p>
        ) : (
          <table className="w-full max-w-md text-left text-sm">
            <thead>
              <tr className="border-border border-b text-xs font-medium">
                <th className="py-1">Date</th>
                <th className="py-1">Cost (USD)</th>
              </tr>
            </thead>
            <tbody>
              {dashboard.aiCostByDay.map((d) => (
                <tr key={d.date} className="border-border border-b">
                  <td className="py-1">{d.date}</td>
                  <td className="py-1">${d.costUsd.toFixed(2)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div>
        <h2 className="text-foreground mb-2 text-sm font-semibold">Storage per project</h2>
        {dashboard.storageByProject.length === 0 ? (
          <p className="text-muted-foreground text-sm">No sessions recorded yet.</p>
        ) : (
          <table className="w-full max-w-md text-left text-sm">
            <thead>
              <tr className="border-border border-b text-xs font-medium">
                <th className="py-1">Project</th>
                <th className="py-1">Total bytes</th>
              </tr>
            </thead>
            <tbody>
              {dashboard.storageByProject.map((p) => (
                <tr key={p.projectId} className="border-border border-b">
                  <td className="py-1">{p.projectName}</td>
                  <td className="py-1">{formatBytes(p.totalBytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
