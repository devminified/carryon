package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// testPaths returns a fresh settings.json path and install-state.json path
// under a temp directory, mirroring ~/.claude/settings.json and
// ~/.carryon/install-state.json without touching the real home directory.
func testPaths(t *testing.T) (settingsPath, statePath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "claude", "settings.json"), filepath.Join(dir, "carryon", "install-state.json")
}

// jsonEqual compares two JSON documents by value, not by byte layout —
// encoding/json doesn't preserve map key order, so a literal string
// comparison would be flaky.
func jsonEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not valid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("want is not valid JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON mismatch.\ngot:  %s\nwant: %s", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func carryonHooksJSON() string {
	return `{
		"Stop": [{"hooks": [{"type": "command", "command": "carryon hook", "timeout": 5}]}],
		"SessionEnd": [{"hooks": [{"type": "command", "command": "carryon hook --final", "timeout": 5}]}]
	}`
}

func TestInstallSettingsGoldenFiles(t *testing.T) {
	tests := []struct {
		name  string
		input string // "" means the file doesn't exist at all
		want  string
	}{
		{
			name:  "no file at all",
			input: "",
			want:  `{"hooks": ` + carryonHooksJSON() + `, "cleanupPeriodDays": 3650}`,
		},
		{
			name:  "empty settings file",
			input: `{}`,
			want:  `{"hooks": ` + carryonHooksJSON() + `, "cleanupPeriodDays": 3650}`,
		},
		{
			name: "existing unrelated hooks and top-level keys are preserved",
			input: `{
				"model": "sonnet",
				"hooks": {
					"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo checking", "timeout": 10}]}]
				}
			}`,
			want: `{
				"model": "sonnet",
				"hooks": {
					"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo checking", "timeout": 10}]}],
					"Stop": [{"hooks": [{"type": "command", "command": "carryon hook", "timeout": 5}]}],
					"SessionEnd": [{"hooks": [{"type": "command", "command": "carryon hook --final", "timeout": 5}]}]
				},
				"cleanupPeriodDays": 3650
			}`,
		},
		{
			name: "an existing Stop hook from another tool is preserved, not replaced",
			input: `{
				"hooks": {
					"Stop": [{"hooks": [{"type": "command", "command": "some-other-tool --on-stop", "timeout": 3}]}]
				}
			}`,
			want: `{
				"hooks": {
					"Stop": [
						{"hooks": [{"type": "command", "command": "some-other-tool --on-stop", "timeout": 3}]},
						{"hooks": [{"type": "command", "command": "carryon hook", "timeout": 5}]}
					],
					"SessionEnd": [{"hooks": [{"type": "command", "command": "carryon hook --final", "timeout": 5}]}]
				},
				"cleanupPeriodDays": 3650
			}`,
		},
		{
			name:  "an existing high cleanupPeriodDays is never lowered",
			input: `{"cleanupPeriodDays": 999999}`,
			want:  `{"hooks": ` + carryonHooksJSON() + `, "cleanupPeriodDays": 999999}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settingsPath, statePath := testPaths(t)
			if tt.input != "" {
				writeFile(t, settingsPath, tt.input)
			}

			if err := InstallSettings(settingsPath, statePath); err != nil {
				t.Fatalf("InstallSettings() error = %v", err)
			}

			got, err := os.ReadFile(settingsPath) // #nosec G304 -- test-controlled temp path
			if err != nil {
				t.Fatalf("read settings.json: %v", err)
			}
			jsonEqual(t, got, []byte(tt.want))
		})
	}
}

func TestInstallSettingsRefusesInvalidJSON(t *testing.T) {
	settingsPath, statePath := testPaths(t)
	writeFile(t, settingsPath, `{not valid json`)

	err := InstallSettings(settingsPath, statePath)
	if err == nil {
		t.Fatal("InstallSettings() with invalid JSON = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %v, want it to explain the file has invalid JSON", err)
	}

	// Refuses, meaning it touches nothing.
	data, readErr := os.ReadFile(settingsPath) // #nosec G304 -- test-controlled temp path
	if readErr != nil {
		t.Fatalf("read settings.json: %v", readErr)
	}
	if string(data) != `{not valid json` {
		t.Errorf("settings.json was modified despite refusing: %s", data)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("install-state.json was written despite refusing")
	}
}

func TestInstallSettingsRefusesReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root ignores permission bits")
	}

	settingsPath, statePath := testPaths(t)
	writeFile(t, settingsPath, `{}`)
	if err := os.Chmod(settingsPath, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	err := InstallSettings(settingsPath, statePath)
	if err == nil {
		t.Fatal("InstallSettings() on a read-only file = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("error = %v, want it to explain the file isn't writable", err)
	}
}

func TestInstallSettingsTwiceIsIdempotent(t *testing.T) {
	settingsPath, statePath := testPaths(t)
	writeFile(t, settingsPath, `{"model": "sonnet"}`)

	if err := InstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("first InstallSettings() error = %v", err)
	}
	firstSettings, _ := os.ReadFile(settingsPath) // #nosec G304
	firstState, _ := os.ReadFile(statePath)       // #nosec G304

	if err := InstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("second InstallSettings() error = %v", err)
	}
	secondSettings, _ := os.ReadFile(settingsPath) // #nosec G304
	secondState, _ := os.ReadFile(statePath)       // #nosec G304

	jsonEqual(t, secondSettings, firstSettings)
	jsonEqual(t, secondState, firstState)

	// No backup files pile up on the idempotent second run.
	matches, err := filepath.Glob(settingsPath + ".carryon-bak-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("got %d backup files after two installs, want exactly 1", len(matches))
	}
}

func TestUninstallSettingsRestoresExactPreInstallContentWhenFileExisted(t *testing.T) {
	settingsPath, statePath := testPaths(t)
	original := `{"model": "sonnet", "tui": "fullscreen"}`
	writeFile(t, settingsPath, original)

	if err := InstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("InstallSettings() error = %v", err)
	}
	if err := UninstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("UninstallSettings() error = %v", err)
	}

	got, err := os.ReadFile(settingsPath) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	jsonEqual(t, got, []byte(original))

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("install-state.json still exists after uninstall")
	}
	matches, _ := filepath.Glob(settingsPath + ".carryon-bak-*")
	if len(matches) != 0 {
		t.Errorf("backup file(s) left behind after uninstall: %v", matches)
	}
}

func TestUninstallSettingsRemovesFileThatDidNotExistBeforeInstall(t *testing.T) {
	settingsPath, statePath := testPaths(t)

	if err := InstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("InstallSettings() error = %v", err)
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json wasn't created: %v", err)
	}

	if err := UninstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("UninstallSettings() error = %v", err)
	}

	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Errorf("settings.json still exists after uninstall, want it removed (it didn't exist before install)")
	}
}

func TestUninstallSettingsWithoutInstallIsANoOp(t *testing.T) {
	settingsPath, statePath := testPaths(t)
	writeFile(t, settingsPath, `{"model": "sonnet"}`)

	if err := UninstallSettings(settingsPath, statePath); err != nil {
		t.Fatalf("UninstallSettings() without a prior install returned error: %v", err)
	}

	got, err := os.ReadFile(settingsPath) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	jsonEqual(t, got, []byte(`{"model": "sonnet"}`))
}
