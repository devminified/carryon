'use client';

import { useRouter } from 'next/navigation';
import type { ProjectRow } from '../types';

export function ProjectSwitcher({
  projects,
  activeProjectId,
}: {
  projects: ProjectRow[];
  activeProjectId?: string;
}) {
  const router = useRouter();

  if (projects.length === 0) {
    return <span className="text-muted-foreground text-sm">No projects yet</span>;
  }

  return (
    <select
      aria-label="Switch project"
      value={activeProjectId ?? ''}
      onChange={(e) => router.push(`/p/${e.target.value}`)}
      className="border-border bg-background text-foreground rounded-md border px-2 py-1 text-sm"
    >
      {!activeProjectId && <option value="">Select a project…</option>}
      {projects.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  );
}
