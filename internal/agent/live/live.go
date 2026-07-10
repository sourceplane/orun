// Package live is the session-body registry (specs/orun-agents-live/design.md
// §3.2): one JSON file per live session under .orun/agents/live/, written by
// the body on start, updated on state change, removed on clean exit. It is
// deliberately ephemeral state, NOT content — never sealed, never synced (the
// refs-vs-objects split). Crash-safety comes from the reader: List sweeps
// entries whose pid is gone, so a kill -9'd body disappears from `orun agent
// ps` without a daemon.
package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Entry describes one live session body.
type Entry struct {
	SessionID string    `json:"sessionId"`
	PID       int       `json:"pid"`
	Socket    string    `json:"socket"`
	State     string    `json:"state"`
	BriefID   string    `json:"briefId,omitempty"`
	AgentType string    `json:"agentType,omitempty"`
	Task      string    `json:"task,omitempty"`
	Driver    string    `json:"driver,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

// ErrNotFound reports a session id with no live entry.
var ErrNotFound = errors.New("live: session not found")

func entryPath(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".json")
}

// Write records (or replaces) a session's entry atomically.
func Write(dir string, e Entry) error {
	if e.SessionID == "" || !strings.HasPrefix(e.SessionID, "as_") {
		return fmt.Errorf("live: bad session id %q", e.SessionID)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := entryPath(dir, e.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, entryPath(dir, e.SessionID))
}

// UpdateState rewrites one entry's state in place. A missing entry is not an
// error (the body may already have cleaned up).
func UpdateState(dir, sessionID, state string) error {
	e, err := Get(dir, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	e.State = state
	return Write(dir, e)
}

// Remove deletes a session's entry (clean exit). Idempotent.
func Remove(dir, sessionID string) {
	_ = os.Remove(entryPath(dir, sessionID))
}

// Get reads one entry without liveness checking.
func Get(dir, sessionID string) (Entry, error) {
	b, err := os.ReadFile(entryPath(dir, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return Entry{}, fmt.Errorf("live: %s: %w", sessionID, err)
	}
	return e, nil
}

// Resolve returns a LIVE entry for the session id: the entry must exist and
// its body must be alive (a dead body's entry is swept on the spot).
func Resolve(dir, sessionID string) (Entry, error) {
	e, err := Get(dir, sessionID)
	if err != nil {
		return Entry{}, err
	}
	if !pidAlive(e.PID) {
		Remove(dir, sessionID)
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// List returns the live entries, sweeping the dead: an entry whose pid no
// longer exists (or whose file is unreadable) is removed rather than shown.
// Sorted by StartedAt, newest first.
func List(dir string) ([]Entry, error) {
	names, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, de := range names {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(de.Name(), ".json")
		e, err := Get(dir, id)
		if err != nil {
			_ = os.Remove(filepath.Join(dir, de.Name()))
			continue
		}
		if !pidAlive(e.PID) {
			Remove(dir, id)
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// pidAlive reports whether a process exists (signal 0 probe).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means it exists but is not ours — alive for sweep purposes.
	return errors.Is(err, syscall.EPERM)
}
