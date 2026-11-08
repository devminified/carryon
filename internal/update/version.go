package update

import (
	"strconv"
	"strings"
)

// compareVersions is a deliberately lenient semver-lite comparator —
// "v1.2.3", "1.2.3", or anything with at least a numeric major
// component compares sanely; it's used only to decide "is this newer"
// and "does this exceed the pin," not to enforce the full semver spec
// (no speculative scale, CLAUDE.md — nothing here needs pre-release
// ordering, build metadata, etc.). Returns -1/0/1, or 0 (treated as
// "not newer, don't update") if either string has no parseable
// leading numeric component — failing closed, never updating on a
// comparison it can't make sense of.
func compareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parseVersion extracts up to 3 numeric components (major, minor,
// patch) from a version string, tolerating a leading "v" and a
// trailing pre-release/build suffix (e.g. "1.2.3-next", goreleaser's
// own snapshot version_template). Missing trailing components default
// to 0 ("1.2" == "1.2.0"). ok is false if even the major component
// isn't a plain non-negative integer.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if dash := strings.IndexAny(v, "-+"); dash >= 0 {
		v = v[:dash]
	}
	// SplitN(v, ".", 3) can never return more than 3 elements, so i is
	// always a valid index into out[3] below — #nosec G602, gosec can't
	// see SplitN's own length bound.
	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n // #nosec G602 -- bounded by SplitN(…, 3) above, see comment
	}
	return out, true
}

// ShouldUpdate decides whether the agent should fetch and apply
// latest, given current and an optional pinned version (""  means no
// pin). The pin is a ceiling, never a target to reach or retreat to:
// if latest exceeds the pin, nothing happens at all — this ticket's
// own "a pinned version stops updates past it." It does not attempt
// to fetch or apply any version other than the one the manifest
// currently names (no speculative "reach historical version X"
// feature — a single "latest" manifest has no other version's assets
// to offer anyway).
func ShouldUpdate(current, latest, pinned string) bool {
	if pinned != "" && compareVersions(latest, pinned) > 0 {
		return false
	}
	return compareVersions(latest, current) > 0
}
