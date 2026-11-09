import { describe, expect, it } from 'vitest';
import { decompressZstd } from './zstd';

// Real zstd frame from internal/upload's own encoder (klauspost/compress/zstd
// level 3, no content-size header) — not a frame zstd-codec produced itself,
// so this proves real interop, not just round-tripping its own output.
const REAL_GO_ZSTD_FRAME_BASE64 =
  'KLUv/QQAEQIAeyJ0eXBlIjoidXNlciIsIm1lc3NhZ2UiOnsicm9sZSI6InVzZXIiLCJjb250ZW50IjoiaGVsbG8gd29ybGQifX0KpObQQA==';

describe('decompressZstd', () => {
  it('decompresses a real Go-produced zstd frame byte-for-byte', async () => {
    const compressed = Uint8Array.from(Buffer.from(REAL_GO_ZSTD_FRAME_BASE64, 'base64'));
    const decompressed = await decompressZstd(compressed);
    const text = Buffer.from(decompressed).toString('utf8');
    expect(text).toBe('{"type":"user","message":{"role":"user","content":"hello world"}}\n');
  });
});
