package update

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0}, // leading "v" is tolerated
		{"1.2.4", "1.2.3", 1},
		{"1.2.3", "1.2.4", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},           // missing patch defaults to 0
		{"1.2.3-next", "1.2.3", 0},    // pre-release suffix stripped
		{"not-a-version", "1.2.3", 0}, // unparseable -> 0, fail closed
		{"1.2.3", "not-a-version", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestShouldUpdateWithNoPin(t *testing.T) {
	if !ShouldUpdate("1.0.0", "1.1.0", "") {
		t.Error("ShouldUpdate(1.0.0 -> 1.1.0, no pin) = false, want true")
	}
	if ShouldUpdate("1.1.0", "1.0.0", "") {
		t.Error("ShouldUpdate(1.1.0 -> 1.0.0, no pin) = true, want false (not an update)")
	}
	if ShouldUpdate("1.0.0", "1.0.0", "") {
		t.Error("ShouldUpdate(same version, no pin) = true, want false")
	}
}

func TestShouldUpdateAPinnedVersionStopsUpdatesPastIt(t *testing.T) {
	// This ticket's own testing point, verbatim.
	if ShouldUpdate("1.0.0", "2.0.0", "1.5.0") {
		t.Error("ShouldUpdate(latest 2.0.0, pinned 1.5.0) = true, want false — must not update past the pin")
	}
}

func TestShouldUpdateCanStillReachThePinFromBelow(t *testing.T) {
	if !ShouldUpdate("1.0.0", "1.2.0", "1.5.0") {
		t.Error("ShouldUpdate(current 1.0.0, latest 1.2.0, pinned 1.5.0) = false, want true — latest is still <= the pin")
	}
}

func TestShouldUpdateExactlyAtThePinIsAllowed(t *testing.T) {
	if !ShouldUpdate("1.0.0", "1.5.0", "1.5.0") {
		t.Error("ShouldUpdate(latest == pin) = false, want true")
	}
}
