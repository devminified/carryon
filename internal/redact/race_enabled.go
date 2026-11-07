//go:build race

package redact

// raceEnabled is true when built with -race. The throughput test uses
// this to skip its 20MB/s assertion under race instrumentation, which
// measurably slows this package's hot path by roughly 10x on its own
// (verified: ~19-20MB/s normally, ~1.5MB/s under -race for the exact
// same code and sample) — a real, expected cost of the instrumentation
// itself, not a regression, and not something any amount of further
// optimization changes. CI's test job runs go test -race ./... for
// every package, so this must not turn into a flaky required check.
const raceEnabled = true
