package preview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Preview states, as the state file and preview_changed carry them.
const (
	StateStarting = "starting"
	StateReady    = "ready"
	StateFailed   = "failed"
	StateStopped  = "stopped"
)

// ErrNoState reports that no preview was ever started in this checkout.
var ErrNoState = errors.New("no preview has been started here")

// State is .orun/preview/state.json.
type State struct {
	Component string    `json:"component"`
	Port      int       `json:"port"`
	Path      string    `json:"path"`
	State     string    `json:"state"`
	Processes []Process `json:"processes"`
	StartedAt string    `json:"startedAt,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// Process is one running component of a preview.
type Process struct {
	Component string `json:"component"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
}

// Dir is where a checkout keeps its preview's state and logs.
func Dir(root string) string { return filepath.Join(root, ".orun", "preview") }

// StatePath is the state file.
func StatePath(root string) string { return filepath.Join(Dir(root), "state.json") }

func lockPath(root string) string { return filepath.Join(Dir(root), ".lock") }

var unsafeFileRune = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// LogPath is one component's log.
func LogPath(root, component string) string {
	return filepath.Join(Dir(root), unsafeFileRune.ReplaceAllString(component, "_")+".log")
}

// ReadState reads the state file; ErrNoState when there is none.
func ReadState(root string) (State, error) {
	b, err := os.ReadFile(StatePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNoState
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("read %s: %w", StatePath(root), err)
	}
	return st, nil
}

// ensureDir creates the preview directory, ignored by git from within: it
// sits in the checkout a coding agent commits from.
func ensureDir(root string) error {
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		return err
	}
	ignore := filepath.Join(Dir(root), ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(ignore, []byte("# written by orun preview: state and logs, never committed\n*\n"), 0o644)
	}
	return nil
}

// writeState replaces the state file atomically, so a reader never sees half.
func writeState(root string, st State) error {
	if st.Processes == nil {
		st.Processes = []Process{}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := ensureDir(root); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(root), ".state-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), StatePath(root))
}
