import { SidePanelSkeleton } from '@/modules/sessions/viewer/ui/side-panel-skeleton';
import { TranscriptSkeleton } from '@/modules/sessions/viewer/ui/transcript-skeleton';

// Shown only while the page's own required data (getSessionTranscript
// + getCurrentUser) is still in flight — matches the real page's
// `flex gap-6` shell so there's no layout shift once it resolves.
export default function Loading() {
  return (
    <div className="flex gap-6">
      <div className="min-w-0 flex-1">
        <TranscriptSkeleton />
      </div>
      <SidePanelSkeleton />
    </div>
  );
}
