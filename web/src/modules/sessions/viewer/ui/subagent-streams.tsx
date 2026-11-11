'use client';

import { useState } from 'react';
import type { ManifestChunk } from '../lib/fetch-transcript';
import { Button } from '@/shared/ui/button';
import { TranscriptView } from './transcript-view';

export function SubagentStreams({
  apiUrl,
  streams,
}: {
  apiUrl: string;
  streams: Record<string, ManifestChunk[]>;
}) {
  const streamIds = Object.keys(streams);
  if (streamIds.length === 0) return null;

  return (
    <div className="mt-6 flex flex-col gap-2">
      <h3 className="text-foreground text-sm font-semibold">Subagents</h3>
      {streamIds.map((streamId) => (
        <SubagentSection
          key={streamId}
          apiUrl={apiUrl}
          streamId={streamId}
          chunks={streams[streamId]}
        />
      ))}
    </div>
  );
}

function SubagentSection({
  apiUrl,
  streamId,
  chunks,
}: {
  apiUrl: string;
  streamId: string;
  chunks: ManifestChunk[];
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-border rounded border">
      <Button
        type="button"
        variant="ghost"
        onClick={() => setOpen((v) => !v)}
        className="h-auto w-full justify-start gap-2 rounded-none px-2 py-1.5 text-sm font-normal"
      >
        <span aria-hidden="true">{open ? '▾' : '▸'}</span>
        Subagent: {streamId}
      </Button>
      {open && (
        <div className="border-border border-t p-2">
          <TranscriptView apiUrl={apiUrl} chunks={chunks} />
        </div>
      )}
    </div>
  );
}
