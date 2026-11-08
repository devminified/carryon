package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartSpawnsANewProcessWithoutExitingThisOne(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "marker")

	src := `package main

import "os"

func main() {
	f, err := os.Create(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	_ = f.Close()
}
`
	srcPath := filepath.Join(dir, "marker.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing marker source: %v", err)
	}

	exePath := filepath.Join(dir, "marker-exe")
	buildOut, err := exec.Command("go", "build", "-o", exePath, srcPath).CombinedOutput() // #nosec G204 -- fixed go-toolchain invocation, test-only
	if err != nil {
		t.Fatalf("building marker program: %v\n%s", err, buildOut)
	}

	if err := Restart(exePath, []string{markerPath}); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(markerPath); err == nil {
			return // the spawned child ran and created the marker file — success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Restart()'s spawned process never created the marker file within 5s")
}
