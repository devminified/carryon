import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { fetchChunkText, streamChunksInOrder, type ManifestChunk } from './fetch-transcript';
import { decompressZstd } from './zstd';

// zstd-codec's WASM init is lazy and memoized per module instance — if
// the first real call happens *after* a test has stubbed the global
// fetch, that stub also intercepts zstd-codec's own internal fetch for
// its WASM binary (real failure mode hit here: "WebAssembly.instantiate:
// expected magic word... found 28 b5 2f fd" — zstd's own frame magic,
// because the mock blindly returned it for *any* fetch call). Warming
// the module with a real, unstubbed call first avoids that entirely.
beforeAll(async () => {
  await decompressZstd(
    Uint8Array.from(
      Buffer.from(
        'KLUv/QQAEQIAeyJ0eXBlIjoidXNlciIsIm1lc3NhZ2UiOnsicm9sZSI6InVzZXIiLCJjb250ZW50IjoiaGVsbG8gd29ybGQifX0KpObQQA==',
        'base64',
      ),
    ),
  );
});

// Two independent real Go-produced zstd frames (klauspost/compress/zstd),
// each a complete line — same real-interop proof as zstd.test.ts, this
// time exercising the full fetch+decompress+verify pipeline.
const FRAME_0 =
  'KLUv/QQAEQIAeyJ0eXBlIjoidXNlciIsIm1lc3NhZ2UiOnsicm9sZSI6InVzZXIiLCJjb250ZW50IjoiaGVsbG8gd29ybGQifX0KpObQQA==';
// sha256 of the frame-0 decompressed bytes, computed once via the same
// real pipeline (web crypto) and pinned here as the expected value.
const SHA_0 = 'dafb8ea63a2a14e6d9b077da68f43d7cc583d3de0ed22526da75749589f21ff3';

function chunk(seq: number, sha256: string): ManifestChunk {
  return {
    streamId: 'main',
    seq,
    byteStart: 0,
    byteEnd: 100,
    sha256,
    objectKey: `s1/main/chunk-${seq}.jsonl.zst`,
    bucket: 'carryon-sessions',
    downloadToken: `token-${seq}`,
  };
}

function mockFetchReturning(base64: string, delayMs = 0) {
  const bytes = Uint8Array.from(Buffer.from(base64, 'base64'));
  return vi.fn().mockImplementation(
    () =>
      new Promise((resolve) =>
        setTimeout(
          () =>
            resolve({
              ok: true,
              status: 200,
              arrayBuffer: () => Promise.resolve(bytes.buffer),
            }),
          delayMs,
        ),
      ),
  );
}

describe('fetchChunkText', () => {
  afterEach(() => vi.unstubAllGlobals());

  it("downloads, decompresses, and verifies a real chunk's sha256", async () => {
    vi.stubGlobal('fetch', mockFetchReturning(FRAME_0));
    const text = await fetchChunkText('http://127.0.0.1:54421', chunk(0, SHA_0));
    expect(text).toBe('{"type":"user","message":{"role":"user","content":"hello world"}}\n');
  });

  it('throws on a sha256 mismatch rather than silently returning tampered content', async () => {
    vi.stubGlobal('fetch', mockFetchReturning(FRAME_0));
    await expect(
      fetchChunkText('http://127.0.0.1:54421', chunk(0, 'f'.repeat(64))),
    ).rejects.toThrow(/sha256 mismatch/);
  });

  it('throws on a non-OK response instead of returning empty content', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 403 }));
    await expect(fetchChunkText('http://127.0.0.1:54421', chunk(0, SHA_0))).rejects.toThrow(
      /HTTP 403/,
    );
  });
});

describe('streamChunksInOrder', () => {
  afterEach(() => vi.unstubAllGlobals());

  it("applies chunks in seq order even when a later chunk's download finishes first", async () => {
    let call = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => {
        call += 1;
        // seq 0's fetch is the slower one — if the implementation
        // applied results in resolution order instead of seq order,
        // this would catch it.
        const delay = call === 1 ? 30 : 0;
        const bytes = Uint8Array.from(Buffer.from(FRAME_0, 'base64'));
        return new Promise((resolve) =>
          setTimeout(
            () =>
              resolve({ ok: true, status: 200, arrayBuffer: () => Promise.resolve(bytes.buffer) }),
            delay,
          ),
        );
      }),
    );

    const seen: number[] = [];
    await streamChunksInOrder('http://127.0.0.1:54421', [chunk(1, SHA_0), chunk(0, SHA_0)], (c) =>
      seen.push(c.seq),
    );
    expect(seen).toEqual([0, 1]);
  });
});
