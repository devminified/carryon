import Link from 'next/link';
import { notFound } from 'next/navigation';
import { getProjectForCurrentUser } from '@/modules/projects/data/queries';
import {
  listBranchesForProject,
  listDevelopersForProject,
  listSessionsForProject,
} from '@/modules/sessions/data/queries';
import { SessionFilters } from '@/modules/sessions/ui/session-filters';
import { SessionTable } from '@/modules/sessions/ui/session-table';
import type {
  SessionListCursor,
  SessionListFilters,
  SessionOutcome,
  SessionStatus,
} from '@/modules/sessions/types';
import { SESSION_OUTCOMES, SESSION_STATUSES } from '@/modules/sessions/types';

function decodeCursor(raw: string | undefined): SessionListCursor | undefined {
  if (!raw) return undefined;
  const [lastActivityAt, id] = raw.split('|');
  if (!lastActivityAt || !id) return undefined;
  return { lastActivityAt, id };
}

function encodeCursor(cursor: SessionListCursor): string {
  return `${cursor.lastActivityAt}|${cursor.id}`;
}

export default async function ProjectPage({
  params,
  searchParams,
}: {
  params: Promise<{ project: string }>;
  searchParams: Promise<Record<string, string | undefined>>;
}) {
  const { project: projectId } = await params;
  const project = await getProjectForCurrentUser(projectId);
  if (!project) notFound();

  const sp = await searchParams;
  const filters: SessionListFilters = {
    dev: sp.dev,
    status: SESSION_STATUSES.includes(sp.status as SessionStatus)
      ? (sp.status as SessionStatus)
      : undefined,
    outcome: SESSION_OUTCOMES.includes(sp.outcome as SessionOutcome)
      ? (sp.outcome as SessionOutcome)
      : undefined,
    branch: sp.branch,
    from: sp.from,
    to: sp.to,
    q: sp.q,
  };
  const cursor = decodeCursor(sp.cursor);

  const [{ items, nextCursor }, developers, branches] = await Promise.all([
    listSessionsForProject(projectId, filters, cursor),
    listDevelopersForProject(projectId),
    listBranchesForProject(projectId),
  ]);

  const nextParams = new URLSearchParams(
    Object.entries({ ...sp }).filter(([k, v]) => v && k !== 'cursor') as [string, string][],
  );
  if (nextCursor) nextParams.set('cursor', encodeCursor(nextCursor));

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-foreground text-lg font-semibold">{project.name}</h1>
      <SessionFilters developers={developers} branches={branches} />
      <SessionTable
        projectId={projectId}
        initialItems={items}
        filters={filters}
        isFirstPage={!cursor}
      />
      {nextCursor && (
        <Link
          href={`/p/${projectId}?${nextParams.toString()}`}
          className="text-muted-foreground self-start text-sm underline"
        >
          Load more
        </Link>
      )}
    </div>
  );
}
