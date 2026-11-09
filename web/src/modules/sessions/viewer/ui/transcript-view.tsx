'use client';

import { useMemo, useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useStream } from '../hooks/use-transcript';
import { buildToolResultMap, visibleEntries } from '../lib/correlate';
import type { ManifestChunk } from '../lib/fetch-transcript';
import { EntryRow } from './entry-row';

// Virtualized so a 200MB session's thousands of entries stay
// responsive (CO-403's own testing point: "first content in under 2
// seconds, smooth scrolling") — only the rows actually on screen (plus
// a small overscan) ever mount, regardless of how many have streamed
// in via useStream's progressive chunk loading.
export function TranscriptView({ apiUrl, chunks }: { apiUrl: string; chunks: ManifestChunk[] }) {
  const { entries, loading, error } = useStream(apiUrl, chunks);
  const visible = useMemo(() => visibleEntries(entries), [entries]);
  const toolResults = useMemo(() => buildToolResultMap(entries), [entries]);

  const parentRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: visible.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 96,
    overscan: 8,
  });

  if (error) {
    return <p className="text-destructive text-sm">Couldn&apos;t load the transcript: {error}</p>;
  }

  if (visible.length === 0 && loading) {
    return (
      <div className="flex flex-col gap-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="border-border bg-border/30 h-16 animate-pulse rounded border" />
        ))}
      </div>
    );
  }

  if (visible.length === 0) {
    return <p className="text-muted-foreground text-sm">This session has no transcript yet.</p>;
  }

  const items = virtualizer.getVirtualItems();

  return (
    <div ref={parentRef} className="h-[70vh] overflow-y-auto" data-testid="transcript-scroll">
      <div style={{ height: virtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
        {items.map((item) => (
          <div
            key={item.key}
            data-index={item.index}
            ref={virtualizer.measureElement}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              transform: `translateY(${item.start}px)`,
            }}
          >
            <EntryRow entry={visible[item.index]} toolResults={toolResults} />
          </div>
        ))}
      </div>
      {loading && <p className="text-muted-foreground py-2 text-center text-xs">Loading more…</p>}
    </div>
  );
}
