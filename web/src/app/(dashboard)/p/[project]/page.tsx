import Link from 'next/link';
import { notFound } from 'next/navigation';
import { getProjectForCurrentUser } from '@/modules/projects/data/queries';
import {
  findSessionsByFile,
  listBranchesForProject,
  listDevelopersForProject,
  listSessionsForProject,
  searchSessionsForProject,
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
import { getCurrentRole } from '@/shared/lib/auth';

function decodeCursor(raw: string | undefined): SessionListCursor | undefined {
  if (!raw) return undefined;
  const [pinned, lastActivityAt, id] = raw.split('|');
  if (!pinned || !lastActivityAt || !id) return undefined;
  return { pinned: pinned === '1', lastActivityAt, id };
}

function encodeCursor(cursor: SessionListCursor): string {
  return `${cursor.pinned ? '1' : '0'}|${cursor.lastActivityAt}|${cursor.id}`;
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
    file: sp.file,
  };
  const cursor = decodeCursor(sp.cursor);

  // CO-501/CO-502: a query switches the page to semantic search
  // results; a file filter switches it to "who touched this file"
  // results — both ranked/matched rather than paginated (see each
  // function's own comment), instead of the plain, cursor-paginated
  // project list. session-filters.tsx keeps q/file mutually exclusive
  // in the URL, so at most one of these ever applies.
  const [{ items, nextCursor }, developers, branches, role] = await Promise.all([
    filters.q
      ? searchSessionsForProject(projectId, { ...filters, q: filters.q }).then((items) => ({
          items,
          nextCursor: null,
        }))
      : filters.file
        ? findSessionsByFile(projectId, { ...filters, file: filters.file }).then((items) => ({
            items,
            nextCursor: null,
          }))
        : listSessionsForProject(projectId, filters, cursor),
    listDevelopersForProject(projectId),
    listBranchesForProject(projectId),
    getCurrentRole(projectId),
  ]);
  const isAdmin = role === 'admin';

  const nextParams = new URLSearchParams(
    Object.entries({ ...sp }).filter(([k, v]) => v && k !== 'cursor') as [string, string][],
  );
  if (nextCursor) nextParams.set('cursor', encodeCursor(nextCursor));

  // CO-601: computed once here, server-side, and threaded all the way
  // down as a prop (SessionTable -> useLiveSessions -> useLiveNow) so
  // the SSR pass and the first client hydration pass render the exact
  // same instant — see use-live-sessions.ts's own comment for why a
  // client-computed `Date.now()` at that layer caused a real
  // hydration mismatch.
  const now = new Date().toISOString();

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-foreground text-lg font-semibold">{project.name}</h1>
      <SessionFilters developers={developers} branches={branches} />
      <SessionTable
        projectId={projectId}
        initialItems={items}
        filters={filters}
        isFirstPage={!cursor}
        isAdmin={isAdmin}
        now={now}
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
