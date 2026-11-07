package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullCommandHasEveryFlag(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"pull"})
	if err != nil {
		t.Fatalf("find pull: %v", err)
	}
	for _, name := range []string{"no-launch", "yes", "clean"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("pull command has no --%s flag", name)
		}
	}
}

func TestPullRequiresExactlyOneArg(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"pull"})
	if err != nil {
		t.Fatalf("find pull: %v", err)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("pull with no args should be rejected")
	}
	if err := cmd.Args(cmd, []string{"one-id"}); err != nil {
		t.Errorf("pull with one positional arg should be accepted, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"one-id", "two-id"}); err == nil {
		t.Error("pull with two positional args should be rejected")
	}
}

func TestPullReportsNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	root := newRootCmd()
	root.SetArgs([]string{"pull", "11111111-1111-1111-1111-111111111111"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("pull with no config = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want it to mention carryon isn't configured", err)
	}
}

func TestCloneURLBuildsAnHTTPSURLFromTheCanonicalRemote(t *testing.T) {
	got := cloneURL("github.com/acme/api")
	want := "https://github.com/acme/api"
	if got != want {
		t.Errorf("cloneURL() = %q, want %q", got, want)
	}
}

func TestStepErrNamesTheStepAndTheRetryCommand(t *testing.T) {
	err := stepErr("manifest", "sess-1", errExample)
	got := err.Error()
	for _, want := range []string{"manifest", "sess-1", "carryon pull sess-1", errExample.Error()} {
		if !strings.Contains(got, want) {
			t.Errorf("stepErr error = %q, want it to contain %q", got, want)
		}
	}
}

var errExample = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestRewriteEveryFileAppliesToMainAndEverySubagentUnderANewSubdir(t *testing.T) {
	dir := t.TempDir()
	mainSrc := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(mainSrc, []byte("main-content\n"), 0o600); err != nil {
		t.Fatalf("writing mainSrc: %v", err)
	}
	subDir := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}
	subSrc := filepath.Join(subDir, "abc123.jsonl")
	if err := os.WriteFile(subSrc, []byte("sub-content\n"), 0o600); err != nil {
		t.Fatalf("writing subSrc: %v", err)
	}

	var gotPairs [][2]string
	apply := func(src, dst string) error {
		gotPairs = append(gotPairs, [2]string{src, dst})
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	}

	result, err := rewriteEveryFile(dir, "pass1", mainSrc, map[string]string{"abc123": subSrc}, apply)
	if err != nil {
		t.Fatalf("rewriteEveryFile() error = %v", err)
	}

	if filepath.Dir(result.main) != filepath.Join(dir, "pass1") {
		t.Errorf("main rewritten into %q, want under %q", result.main, filepath.Join(dir, "pass1"))
	}
	got, err := os.ReadFile(result.main)
	if err != nil || string(got) != "main-content\n" {
		t.Errorf("rewritten main content = %q, err = %v", got, err)
	}

	subDst, ok := result.subagents["abc123"]
	if !ok {
		t.Fatalf("subagents map missing abc123: %+v", result.subagents)
	}
	got, err = os.ReadFile(subDst)
	if err != nil || string(got) != "sub-content\n" {
		t.Errorf("rewritten subagent content = %q, err = %v", got, err)
	}

	if len(gotPairs) != 2 {
		t.Errorf("apply called %d times, want 2", len(gotPairs))
	}
}

func TestRewriteEveryFilePropagatesAnApplyError(t *testing.T) {
	dir := t.TempDir()
	mainSrc := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(mainSrc, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing mainSrc: %v", err)
	}

	_, err := rewriteEveryFile(dir, "pass1", mainSrc, nil, func(src, dst string) error {
		return errExample
	})
	if err == nil {
		t.Fatal("rewriteEveryFile() error = nil, want the apply error propagated")
	}
}
