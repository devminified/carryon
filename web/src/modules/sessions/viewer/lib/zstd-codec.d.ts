declare module 'zstd-codec' {
  export const ZstdCodec: {
    run(callback: (zstd: unknown) => void): void;
  };
}
