import { getSessionFamily, getSessionTranscript } from '@/modules/sessions/viewer/data/queries';
import { groupChunksByStream } from '@/modules/sessions/viewer/lib/group-chunks';
import { SidePanel } from '@/modules/sessions/viewer/ui/side-panel';
import { SubagentStreams } from '@/modules/sessions/viewer/ui/subagent-streams';
import { TranscriptView } from '@/modules/sessions/viewer/ui/transcript-view';
import { getCurrentRole, getCurrentUser } from '@/shared/lib/auth';

export default async function SessionViewerPage({ params }: { params: Promise<{ id: string }> }) {
  const { id: sessionId } = await params;
  // getSessionTranscript is the one call that must happen — it writes
  // get-session-transcript's own "view" audit row server-side (CO-403's
  // own testing point: once per open). getSessionFamily (CO-404) is
  // independent supplementary tree data for the same open, not a
  // second audited visit; getCurrentUser doesn't depend on the session
  // at all — all three run in parallel.
  const [{ session, chunks }, { nodes: familyNodes, danglingParentId }, currentUser] =
    await Promise.all([
      getSessionTranscript(sessionId),
      getSessionFamily(sessionId),
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
        familyNodes={familyNodes}
        danglingParentId={danglingParentId}
        canRename={canRename}
        now={now}
      />
    </div>
  );
}
