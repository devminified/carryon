import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';

// A plain GET <form> — reloading the page with query params is the
// whole filter mechanism, no client JS needed (data-fetching.md:
// Server Components fetch, this page included). The CSV export link
// below carries the exact same query string, which is what makes
// "CSV export matches the filtered view" (this ticket's own testing
// point) true by construction rather than by keeping two filter
// implementations in sync.
//
// CO-604: the <select>s and <label>s here stay native, not shadcn's
// <Select>/<Label> — both of those are Radix-backed Client Components,
// and adopting them would force this whole page to become one too,
// defeating the zero-JS design above. Button and Input are both RSC-
// safe (no 'use client' in either's generated source) so those two
// swap in without that cost.
export function AuditFilters({
  actions,
  projects,
  current,
}: {
  actions: string[];
  projects: { id: string; name: string }[];
  current: { actorEmail?: string; action?: string; projectId?: string; since?: string; until?: string };
}) {
  const exportQuery = new URLSearchParams(
    Object.fromEntries(Object.entries(current).filter(([, v]) => v !== undefined)) as Record<string, string>,
  ).toString();

  return (
    <form method="get" className="border-border mb-4 flex flex-wrap items-end gap-3 border-b pb-4">
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Actor email</span>
        <Input name="actorEmail" defaultValue={current.actorEmail ?? ''} className="w-auto" />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Action</span>
        <select name="action" defaultValue={current.action ?? ''} className="border-border rounded border px-2 py-1 text-sm">
          <option value="">All</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Project</span>
        <select name="projectId" defaultValue={current.projectId ?? ''} className="border-border rounded border px-2 py-1 text-sm">
          <option value="">All</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Since</span>
        <Input type="date" name="since" defaultValue={current.since ?? ''} className="w-auto" />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium">Until</span>
        <Input type="date" name="until" defaultValue={current.until ?? ''} className="w-auto" />
      </label>
      <Button type="submit">Apply filters</Button>
      <a
        href={`/api/admin/audit/export${exportQuery ? `?${exportQuery}` : ''}`}
        className="text-muted-foreground hover:text-foreground text-sm underline"
      >
        Export CSV
      </a>
    </form>
  );
}
