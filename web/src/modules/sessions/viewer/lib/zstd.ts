import { ZstdCodec } from 'zstd-codec';

type ZstdModule = {
  Streaming: new () => { decompress: (bytes: Uint8Array, sizeHint?: number) => Uint8Array };
};

let modulePromise: Promise<ZstdModule> | null = null;

// zstd-codec's only entry point is callback-based (ZstdCodec.run), with
// its WASM binary embedded as a base64 data: URI in the JS module
// itself (verified: no separate .wasm asset, no bundler config
// needed). Memoized so every chunk decompression on a page reuses one
// initialized instance instead of re-instantiating WASM per chunk.
function zstdModule(): Promise<ZstdModule> {
  if (!modulePromise) {
    modulePromise = new Promise((resolve) => {
      ZstdCodec.run((zstd) => resolve(zstd as unknown as ZstdModule));
    });
  }
  return modulePromise;
}

// zstd.Simple().decompress() returns null against a real frame
// produced by klauspost/compress/zstd (internal/upload's own encoder,
// go-conventions.md) — that encoder doesn't write a content-size
// header, which Simple's binding apparently requires. zstd.Streaming's
// decompress() doesn't have that requirement (verified against a real
// Go-encoded frame, byte-for-byte), so that's the one this uses.
export async function decompressZstd(compressed: Uint8Array): Promise<Uint8Array> {
  const zstd = await zstdModule();
  const streaming = new zstd.Streaming();
  return streaming.decompress(compressed);
}
