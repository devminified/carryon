'use client';

import { useParams, useRouter } from 'next/navigation';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import type { ProjectRow } from '../types';

// CO-703: upgraded from a bare native <select> to a real shadcn
// Select (CO-604's own precedent — a native control is fine for
// read-only display, but this is real data selection). Also fixes a
// real, previously-identified bug: the old activeProjectId prop was
// declared but never actually passed by any caller, so the switcher
// always showed its placeholder even while viewing a real project.
// useParams() reads the CURRENT route's own dynamic segment directly
// — it works from any Client Component in the tree regardless of
// which layout renders it, so there's no prop-threading needed at
// all: on /p/[project] it's the real project id, everywhere else
// (including /sessions/[id], which has no "project" param) it's
// correctly undefined.
export function ProjectSwitcher({ projects }: { projects: ProjectRow[] }) {
  const router = useRouter();
  const { project: activeProjectId } = useParams<{ project?: string }>();

  if (projects.length === 0) {
    return <span className="text-muted-foreground text-sm">No projects yet</span>;
  }

  return (
    <Select value={activeProjectId ?? ''} onValueChange={(id) => router.push(`/p/${id}`)}>
      <SelectTrigger aria-label="Switch project" size="sm" className="w-auto min-w-40">
        <SelectValue placeholder="Select a project…" />
      </SelectTrigger>
      <SelectContent>
        {projects.map((p) => (
          <SelectItem key={p.id} value={p.id}>
            {p.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
