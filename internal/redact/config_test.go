package redact

import "testing"

func TestApplyServerConfigDisablesRuleByID(t *testing.T) {
	base := DefaultConfig()
	before := len(base.Rules)

	out := ApplyServerConfig(base, []string{"generic-api-key"}, nil)

	if len(out.Rules) != before-1 {
		t.Fatalf("len(out.Rules) = %d, want %d", len(out.Rules), before-1)
	}
	for _, r := range out.Rules {
		if r.ID == "generic-api-key" {
			t.Fatal("generic-api-key rule still present after being disabled")
		}
	}
}

func TestApplyServerConfigAppendsDenylistAdditions(t *testing.T) {
	base := DefaultConfig()
	before := len(base.Denylist)

	out := ApplyServerConfig(base, nil, []string{"*.secret"})

	if len(out.Denylist) != before+1 {
		t.Fatalf("len(out.Denylist) = %d, want %d", len(out.Denylist), before+1)
	}
	if out.Denylist[len(out.Denylist)-1] != "*.secret" {
		t.Fatalf("last denylist entry = %q, want \"*.secret\"", out.Denylist[len(out.Denylist)-1])
	}
	// base's own denylist is untouched.
	if len(base.Denylist) != before {
		t.Fatalf("base.Denylist mutated: len = %d, want %d", len(base.Denylist), before)
	}
}

func TestApplyServerConfigNoChangesLeavesBaseEquivalent(t *testing.T) {
	base := DefaultConfig()
	out := ApplyServerConfig(base, nil, nil)

	if len(out.Rules) != len(base.Rules) {
		t.Fatalf("len(out.Rules) = %d, want %d", len(out.Rules), len(base.Rules))
	}
	if len(out.Denylist) != len(base.Denylist) {
		t.Fatalf("len(out.Denylist) = %d, want %d", len(out.Denylist), len(base.Denylist))
	}
}

func TestApplyServerConfigUnknownDisabledIDIsIgnored(t *testing.T) {
	base := DefaultConfig()
	out := ApplyServerConfig(base, []string{"no-such-rule"}, nil)

	if len(out.Rules) != len(base.Rules) {
		t.Fatalf("len(out.Rules) = %d, want %d (unknown ID shouldn't drop anything)", len(out.Rules), len(base.Rules))
	}
}
