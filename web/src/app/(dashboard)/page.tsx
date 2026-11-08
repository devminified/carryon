import Link from 'next/link';
import { listProjectsForCurrentUser } from '@/modules/projects/data/queries';

export default async function OverviewPage() {
  const projects = await listProjectsForCurrentUser();

  if (projects.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        You&apos;re not a member of any project yet. Ask an admin to add you, or register one with{' '}
        <code className="bg-border rounded px-1 py-0.5">carryon admin project add</code>.
      </p>
    );
  }

  return (
    <ul className="flex flex-col gap-2">
      {projects.map((p) => (
        <li key={p.id}>
          <Link href={`/p/${p.id}`} className="text-foreground underline">
            {p.name}
          </Link>
        </li>
      ))}
    </ul>
  );
}
