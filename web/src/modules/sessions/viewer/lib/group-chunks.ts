import type { ManifestChunk } from './fetch-transcript';

// Plain data transform, not client-only — the page (a Server Component)
// calls this directly to split chunks before handing each stream's
// slice to a client component; it can't live in use-transcript.ts,
// whose "use client" directive makes every export there a client
// reference (Next's own error #441: a server caller can only render a
// client export as a Component, never call it as a plain function).
export function groupChunksByStream(chunks: ManifestChunk[]): Record<string, ManifestChunk[]> {
  const byStream: Record<string, ManifestChunk[]> = {};
  for (const c of chunks) {
    (byStream[c.streamId] ??= []).push(c);
  }
  return byStream;
}
