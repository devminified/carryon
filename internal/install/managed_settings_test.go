package install

// CO-308: packaging/managed-settings/managed-settings.json is the
// deployable content an MDM admin copies to Claude Code's real
// enterprise-managed settings path (macOS: /Library/Application
// Support/ClaudeCode/managed-settings.json; Linux:
// /etc/claude-code/managed-settings.json; Windows: C:\Program
// Files\ClaudeCode\managed-settings.json — see
// docs/co-308-fleet-rollout.md). This test guards against it ever
// drifting from the actual hooks InstallSettings merges into a user's
// own settings.json — a managed-settings.json with stale hooks would
// silently stop enforcing anything the moment internal/install's own
// hook shape changes, with no test failure anywhere else to catch it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedSettingsJSONMatchesInstallSettingsOwnHookShape(t *testing.T) {
	path := filepath.Join("..", "..", "packaging", "managed-settings", "managed-settings.json")
	body, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative path, test-only
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var doc struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}

	jsonEqual(t, doc.Hooks, []byte(carryonHooksJSON()))
}
