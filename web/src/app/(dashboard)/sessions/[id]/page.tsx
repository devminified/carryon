import { getSessionFamily, getSessionTranscript } from '@/modules/sessions/viewer/data/queries';
import { groupChunksByStream } from '@/modules/sessions/viewer/lib/group-chunks';
import { SidePanel } from '@/modules/sessions/viewer/ui/side-panel';
import { SubagentStreams } from '@/modules/sessions/viewer/ui/subagent-streams';
import { TranscriptView } from '@/modules/sessions/viewer/ui/transcript-view';

export default async function SessionViewerPage({ params }: { params: Promise<{ id: string }> }) {
  const { id: sessionId } = await params;
  // getSessionTranscript is the one call that must happen — it writes
  // get-session-transcript's own "view" audit row server-side (CO-403's
  // own testing point: once per open). getSessionFamily (CO-404) is
  // independent supplementary tree data for the same open, not a
  // second audited visit, so both run in parallel.
  const [{ session, chunks }, { nodes: familyNodes, danglingParentId }] = await Promise.all([
    getSessionTranscript(sessionId),
    getSessionFamily(sessionId),
  ]);

  const byStream = groupChunksByStream(chunks);
  const mainChunks = byStream.main ?? [];
  delete byStream.main;

  const apiUrl = process.env.NEXT_PUBLIC_SUPABASE_URL!;

  return (
    <div className="flex gap-6">
      <div className="min-w-0 flex-1">
        <TranscriptView apiUrl={apiUrl} chunks={mainChunks} />
        <SubagentStreams apiUrl={apiUrl} streams={byStream} />
      </div>
      <SidePanel session={session} familyNodes={familyNodes} danglingParentId={danglingParentId} />
    </div>
  );
}
