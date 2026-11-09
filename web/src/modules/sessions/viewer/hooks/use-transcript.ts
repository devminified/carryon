'use client';

import { useEffect, useState } from 'react';
import { parseTranscript, type TranscriptEntry } from '../lib/parse-transcript';
import { streamChunksInOrder, type ManifestChunk } from '../lib/fetch-transcript';

export type StreamState = {
  entries: TranscriptEntry[];
  loading: boolean;
  error: string | null;
};

// Progressively loads one stream's chunks (CO-403's own "loads chunks
// progressively... first content in under 2 seconds"): each chunk's
// lines are parsed and appended to state as soon as that chunk's
// download+decompress+verify resolves, in seq order (streamChunksInOrder
// already guarantees that even when a later chunk's network request
// finishes first) — so the first chunk's messages render immediately
// instead of waiting for the whole stream to download.
export function useStream(apiUrl: string, chunks: ManifestChunk[]): StreamState {
  const chunksKey = chunks.map((c) => `${c.streamId}:${c.seq}`).join(',');

  // Adjusting state when a prop changes (react.dev's own pattern for
  // this, same as use-live-sessions.ts) — a new chunksKey means a
  // different stream (a different session, or this one reloaded), so
  // reset synchronously during render rather than via an effect's own
  // setState call (react-hooks/set-state-in-effect).
  const [prevChunksKey, setPrevChunksKey] = useState(chunksKey);
  const [entries, setEntries] = useState<TranscriptEntry[]>([]);
  const [loading, setLoading] = useState(chunks.length > 0);
  const [error, setError] = useState<string | null>(null);
  if (chunksKey !== prevChunksKey) {
    setPrevChunksKey(chunksKey);
    setEntries([]);
    setLoading(chunks.length > 0);
    setError(null);
  }

  useEffect(() => {
    if (chunks.length === 0) return;
    let cancelled = false;

    streamChunksInOrder(apiUrl, chunks, (_chunk, text) => {
      if (cancelled) return;
      const parsed = parseTranscript(text);
      setEntries((prev) => [...prev, ...parsed]);
    })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chunksKey, apiUrl]);

  return { entries, loading, error };
}
