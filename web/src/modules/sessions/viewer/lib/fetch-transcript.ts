import { decompressZstd } from './zstd';

export type ManifestChunk = {
  streamId: string;
  seq: number;
  byteStart: number;
  byteEnd: number;
  sha256: string;
  objectKey: string;
  bucket: string;
  downloadToken: string;
};

// Same URL shape as internal/api.Client.StorageDownloadURL (chunk.go's
// own doc comment: the signed URL an Edge Function returns bakes in its
// *internal* SUPABASE_URL, unreachable here — only the bare token is
// safe to use, built against this browser's own correct apiUrl).
function downloadUrl(apiUrl: string, chunk: ManifestChunk): string {
  return `${apiUrl}/storage/v1/object/sign/${chunk.bucket}/${chunk.objectKey}?token=${encodeURIComponent(chunk.downloadToken)}`;
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  // .slice() guarantees a plain ArrayBuffer-backed view — crypto.subtle.digest's
  // BufferSource type rejects the SharedArrayBuffer-compatible view
  // decompressZstd's return type allows.
  const digest = await crypto.subtle.digest('SHA-256', bytes.slice());
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// Downloads, decompresses and verifies one chunk — the same
// download/decompress/verify shape internal/pull.downloadAndVerify
// uses, just in the browser (I4: integrity is verifiable end to end).
export async function fetchChunkText(apiUrl: string, chunk: ManifestChunk): Promise<string> {
  const res = await fetch(downloadUrl(apiUrl, chunk));
  if (!res.ok) {
    throw new Error(`chunk seq ${chunk.seq} download failed: HTTP ${res.status}`);
  }
  const compressed = new Uint8Array(await res.arrayBuffer());
  const decompressed = await decompressZstd(compressed);
  const gotSha = await sha256Hex(decompressed);
  if (gotSha !== chunk.sha256) {
    throw new Error(
      `chunk seq ${chunk.seq}: sha256 mismatch (got ${gotSha}, want ${chunk.sha256})`,
    );
  }
  return new TextDecoder().decode(decompressed);
}

// Every chunk is cut at a line boundary in the *original* file
// (internal/upload.uploadStream's own doc comment) — redaction can
// change a line's length but never splits one across two chunks — so
// each decompressed chunk's text is independently valid, complete
// JSONL on its own. That's what makes real progressive loading safe:
// onChunk(seq, text) can be called the moment each chunk resolves,
// without buffering a partial line across the boundary.
export async function streamChunksInOrder(
  apiUrl: string,
  chunks: ManifestChunk[],
  onChunk: (chunk: ManifestChunk, text: string) => void,
): Promise<void> {
  const sorted = [...chunks].sort((a, b) => a.seq - b.seq);
  // Kick off every download in parallel (network-bound), but apply
  // results to the caller strictly in seq order, so the transcript
  // never appears to render out of order even though chunk 3 might
  // finish downloading before chunk 1 does.
  const pending = sorted.map((chunk) => fetchChunkText(apiUrl, chunk));
  for (let i = 0; i < sorted.length; i++) {
    const text = await pending[i];
    onChunk(sorted[i], text);
  }
}
