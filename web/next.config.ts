import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Cache Components (scaffolded on by default in Next 16) requires every
  // session/cookie read to sit behind a Suspense boundary and every cached
  // value to carry an explicit "use cache" directive — a real rendering
  // model change, not a toggle. Carryon's web app is a small, mostly
  // dynamic RLS-scoped list/viewer (data-fetching.md) with no prerendered-
  // shell requirement any current ticket asks for, so this stays off:
  // CLAUDE.md's "no speculative scale" applies to adopting a new rendering
  // model too, not just infra. Left off deliberately, not an oversight.
  turbopack: {
    rules: {
      '*.css': {
        loaders: ['@tailwindcss/turbopack'],
        as: '*.css',
      },
    },
    // zstd-codec (CO-403's WASM zstd decoder) ships one Emscripten
    // bundle with both a Node and a browser code path, require()-ing
    // "fs"/"path" inside a runtime `if (ENVIRONMENT_HAS_NODE)` check
    // that's always false in a real browser — but Turbopack still
    // needs those requires to resolve *something* to bundle the
    // client build. See src/shared/node-builtin-stub.js.
    resolveAlias: {
      fs: './src/shared/node-builtin-stub.js',
      path: './src/shared/node-builtin-stub.js',
    },
  },
};

export default nextConfig;
