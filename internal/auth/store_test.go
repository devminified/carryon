package auth

import (
	"errors"
	"testing"
)

// fakeBackend is an in-memory backend for testing store's fallback logic
// without touching a real OS keyring or filesystem.
type fakeBackend struct {
	value       string
	hasValue    bool
	unavailable error // non-nil: get/set always fail with this
}

func (f *fakeBackend) get() (string, error) {
	if f.unavailable != nil {
		return "", f.unavailable
	}
	if !f.hasValue {
		return "", errNoCredential
	}
	return f.value, nil
}

func (f *fakeBackend) set(v string) error {
	if f.unavailable != nil {
		return f.unavailable
	}
	f.value, f.hasValue = v, true
	return nil
}

func (f *fakeBackend) delete() error {
	f.hasValue = false
	f.value = ""
	return nil
}

func TestStoreUsesKeyringWhenAvailable(t *testing.T) {
	primary := &fakeBackend{}
	fallback := &fakeBackend{}
	s := &store{primary: primary, fallback: fallback}

	if err := s.set("token-a"); err != nil {
		t.Fatalf("set() error = %v", err)
	}
	if primary.value != "token-a" {
		t.Errorf("primary.value = %q, want %q", primary.value, "token-a")
	}
	if fallback.hasValue {
		t.Error("fallback should be untouched when the primary backend works")
	}

	got, err := s.get()
	if err != nil || got != "token-a" {
		t.Errorf("get() = (%q, %v), want (%q, nil)", got, err, "token-a")
	}
}

func TestStoreFallsBackWhenKeyringUnavailable(t *testing.T) {
	primary := &fakeBackend{unavailable: errors.New("no Secret Service running")}
	fallback := &fakeBackend{}
	var warnings []string
	s := &store{primary: primary, fallback: fallback, warn: func(msg string) { warnings = append(warnings, msg) }}

	if err := s.set("token-b"); err != nil {
		t.Fatalf("set() error = %v", err)
	}
	if fallback.value != "token-b" {
		t.Errorf("fallback.value = %q, want %q", fallback.value, "token-b")
	}
	if len(warnings) != 1 {
		t.Errorf("expected exactly one warning on fallback, got %d: %v", len(warnings), warnings)
	}

	got, err := s.get()
	if err != nil || got != "token-b" {
		t.Errorf("get() = (%q, %v), want (%q, nil)", got, err, "token-b")
	}

	// A second set() while still unavailable should not warn again.
	if err := s.set("token-c"); err != nil {
		t.Fatalf("set() error = %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("expected the warning only once per process, got %d: %v", len(warnings), warnings)
	}
}

func TestStoreGetChecksFileWhenKeyringEmpty(t *testing.T) {
	// Covers: a previous run fell back to the file (keyring was down then),
	// and now the keyring is back up but still empty — get() must still
	// find the token that's sitting in the file.
	primary := &fakeBackend{} // available, but empty
	fallback := &fakeBackend{value: "token-from-file", hasValue: true}
	s := &store{primary: primary, fallback: fallback}

	got, err := s.get()
	if err != nil || got != "token-from-file" {
		t.Errorf("get() = (%q, %v), want (%q, nil)", got, err, "token-from-file")
	}
}

func TestStoreGetNotLoggedIn(t *testing.T) {
	s := &store{primary: &fakeBackend{}, fallback: &fakeBackend{}}
	if _, err := s.get(); !errors.Is(err, errNoCredential) {
		t.Errorf("get() error = %v, want errNoCredential", err)
	}
}

func TestStoreDeleteClearsBothBackends(t *testing.T) {
	primary := &fakeBackend{value: "x", hasValue: true}
	fallback := &fakeBackend{value: "y", hasValue: true}
	s := &store{primary: primary, fallback: fallback}

	if err := s.delete(); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
	if primary.hasValue || fallback.hasValue {
		t.Error("delete() should clear both backends")
	}
}

func TestFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fb := fileBackend{path: dir + "/credentials"}

	if _, err := fb.get(); !errors.Is(err, errNoCredential) {
		t.Fatalf("get() before set = %v, want errNoCredential", err)
	}

	if err := fb.set("secret-token"); err != nil {
		t.Fatalf("set() error = %v", err)
	}
	got, err := fb.get()
	if err != nil || got != "secret-token" {
		t.Fatalf("get() = (%q, %v), want (%q, nil)", got, err, "secret-token")
	}

	if err := fb.delete(); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
	if _, err := fb.get(); !errors.Is(err, errNoCredential) {
		t.Errorf("get() after delete = %v, want errNoCredential", err)
	}
}
