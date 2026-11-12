import { Suspense } from 'react';
import { getSessionFamily, getSessionTranscript } from '@/modules/sessions/viewer/data/queries';
import { groupChunksByStream } from '@/modules/sessions/viewer/lib/group-chunks';
import { FamilyTree } from '@/modules/sessions/viewer/ui/family-tree';
import { FamilyTreeSkeleton } from '@/modules/sessions/viewer/ui/family-tree-skeleton';
import { SidePanel } from '@/modules/sessions/viewer/ui/side-panel';
import { SubagentStreams } from '@/modules/sessions/viewer/ui/subagent-streams';
import { TranscriptView } from '@/modules/sessions/viewer/ui/transcript-view';
import { getCurrentRole, getCurrentUser } from '@/shared/lib/auth';

// CO-608: a page-local async Server Component, not a ui/ file —
// layers.md forbids ui/ importing data/queries.ts directly (data
// must reach ui/ as props from a Server Component); this one *is*
// that Server Component, wrapped in its own <Suspense> below so
// get-session-family's fetch can stream in independently of the rest
// of the page.
async function FamilyTreeSection({ sessionId, now }: { sessionId: string; now: string }) {
  const { nodes, danglingParentId } = await getSessionFamily(sessionId);
  return <FamilyTree nodes={nodes} danglingParentId={danglingParentId} now={new Date(now)} />;
}

export default async function SessionViewerPage({ params }: { params: Promise<{ id: string }> }) {
  const { id: sessionId } = await params;
  // getSessionTranscript is the one call that must happen — it writes
  // get-session-transcript's own "view" audit row server-side (CO-403's
  // own testing point: once per open), so it can't be duplicated or
  // deferred behind its own Suspense boundary without double-auditing
  // the same page view. getCurrentUser doesn't depend on the session at
  // all — both run in parallel.
  //
  // CO-608: getSessionFamily (CO-404) is deliberately NOT joined into
  // this Promise.all — it's independent supplementary tree data (no
  // audit row of its own, see that function's own comment), so it's
  // fetched inside FamilyTreeSection instead, wrapped in its own
  // <Suspense> below, letting the transcript and the rest of the side
  // panel render without waiting on it.
  const [{ session, chunks }, currentUser] = await Promise.all([
    getSessionTranscript(sessionId),
    getCurrentUser(),
  ]);

  // CO-405: who may rename this session — its own owner (by GitHub
  // login; get-session-transcript's response has no raw owner user_id
  // to compare directly, see that function's own response shape) or an
  // admin of its project. Depends on session.projectId, so this can't
  // join the Promise.all above.
  const role = currentUser ? await getCurrentRole(session.projectId) : null;
  const canRename = !!currentUser && (currentUser.githubLogin === session.ownerLogin || role === 'admin');

  const byStream = groupChunksByStream(chunks);
  const mainChunks = byStream.main ?? [];
  delete byStream.main;

  const apiUrl = process.env.NEXT_PUBLIC_SUPABASE_URL!;

  // CO-601: computed once here, server-side, and threaded down to
  // SidePanel/FamilyTree as a prop — see use-live-now.ts's own comment
  // for why a bare client-side `new Date()`/`formatRelativeTime(...)`
  // call inside those Client Components caused a real hydration
  // mismatch.
  const now = new Date().toISOString();

  return (
    <div className="flex gap-6">
      <div className="min-w-0 flex-1">
        <TranscriptView apiUrl={apiUrl} chunks={mainChunks} />
        <SubagentStreams apiUrl={apiUrl} streams={byStream} />
      </div>
      <SidePanel
        session={session}
        familyTree={
          <Suspense fallback={<FamilyTreeSkeleton />}>
            <FamilyTreeSection sessionId={sessionId} now={now} />
          </Suspense>
        }
        canRename={canRename}
        now={now}
      />
    </div>
  );
}
