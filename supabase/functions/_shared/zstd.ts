// CO-306's own zstd codec: `rescan` is the one Edge Function that ever
// reads and rewrites a chunk's actual bytes (every other function only
// hands out signed URLs — bytes bypass the API, I9 — so this is a
// deliberate, narrow exception for a background security-remediation
// job operating on its own stored data, not a laptop-facing proxy; see
// redaction.md's rescan section).
//
// @bokuweb/zstd-wasm (pure WASM, no native bindings) is the chosen
// package — verified for real, not assumed, that it round-trips with
// the laptop's own `klauspost/compress/zstd`: a frame this package
// compresses decodes cleanly with klauspost's decoder, and a frame
// klauspost produces decodes cleanly here, both checked with a real
// Go binary during this ticket's own testing. `init()` runs the WASM
// module load once per cold start.
import {
  compress as zstdCompress,
  decompress as zstdDecompress,
  init,
} from "npm:@bokuweb/zstd-wasm@0.0.27";

let initialized = false;

async function ensureInit(): Promise<void> {
  if (!initialized) {
    await init();
    initialized = true;
  }
}

/** Decompresses a zstd frame (as produced by the laptop's own encoder, or by compressChunk below) into its original bytes. */
export async function decompressChunk(data: Uint8Array): Promise<Uint8Array> {
  await ensureInit();
  return zstdDecompress(data);
}

/** Compresses plaintext bytes into a zstd frame decodable by the laptop's own klauspost/compress/zstd decoder — level 3, matching internal/upload's own choice (testing.md/commands.md). */
export async function compressChunk(data: Uint8Array): Promise<Uint8Array> {
  await ensureInit();
  return zstdCompress(data, 3);
}

/** sha256Hex is session_chunks.sha256's own format: lowercase hex of the SHA-256 digest of the *uncompressed* bytes (chunk.ts's own doc comment). */
export async function sha256Hex(data: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", data as BufferSource);
  return Array.from(new Uint8Array(digest)).map((b) =>
    b.toString(16).padStart(2, "0")
  ).join("");
}
