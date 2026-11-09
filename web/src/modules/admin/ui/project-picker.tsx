'use client';

import { useRouter, usePathname, useSearchParams } from 'next/navigation';

export function ProjectPicker({ projects }: { projects: { id: string; name: string }[] }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  return (
    <select
      aria-label="Project"
      value={searchParams.get('project') ?? ''}
      onChange={(e) => router.push(`${pathname}?project=${e.target.value}`)}
      className="border-border bg-background rounded-md border px-2 py-1 text-sm"
    >
      <option value="">Select a project…</option>
      {projects.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  );
}
