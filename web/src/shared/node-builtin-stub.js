// zstd-codec's Emscripten build requires("fs")/require("path") inside an
// `if (ENVIRONMENT_HAS_NODE)` branch that never runs in a real browser —
// but Turbopack still has to resolve the require() statically to bundle
// the client build at all. These two Node builtins are aliased here
// (next.config.ts's turbopack.resolveAlias) to this empty stub rather
// than a real polyfill, since the guarded code path is provably dead
// in a browser context.
module.exports = {};
