package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Prefs is the user-tweakable cockpit preferences persisted between sessions.
type Prefs struct {
	SidebarCollapsed   bool `json:"sidebarCollapsed"`
	InspectorVisible   bool `json:"inspectorVisible"`
	BottomPanelVisible bool `json:"bottomPanelVisible,omitempty"`
	// SelectedEnv is the cockpit's last-used environment (environments.md §1):
	// re-applied on open as the default selected env when it still names a real
	// environment in the workspace.
	SelectedEnv  string                    `json:"selectedEnv,omitempty"`
	PerComponent map[string]ComponentPrefs `json:"perComponent,omitempty"`
}

// ComponentPrefs is the per-component sticky state for Component Studio: the
// last env, trigger, and changed-only toggle the user picked. Re-applied when
// the user re-opens the studio for that component.
type ComponentPrefs struct {
	Env         string `json:"env,omitempty"`
	Trigger     string `json:"trigger,omitempty"`
	ChangedOnly bool   `json:"changedOnly,omitempty"`
}

// DefaultPrefs returns the canonical defaults for a fresh user.
func DefaultPrefs() Prefs {
	return Prefs{
		SidebarCollapsed: false,
		InspectorVisible: true,
		PerComponent:     map[string]ComponentPrefs{},
	}
}

func prefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".orun", "cockpit.json"), nil
}

// LoadPrefs reads ~/.orun/cockpit.json, returning DefaultPrefs() if the file
// is missing or corrupt. Never returns an error to callers — failures are
// best-effort and silent so a hostile filesystem can't break the cockpit.
func LoadPrefs() Prefs {
	p := DefaultPrefs()
	path, err := prefsPath()
	if err != nil {
		return p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var got Prefs
	if err := json.Unmarshal(data, &got); err != nil {
		return p
	}
	if got.PerComponent == nil {
		got.PerComponent = map[string]ComponentPrefs{}
	}
	return got
}

// SavePrefs persists prefs to ~/.orun/cockpit.json, creating the directory
// if needed. Errors are swallowed — prefs are non-critical.
func SavePrefs(p Prefs) {
	path, err := prefsPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
