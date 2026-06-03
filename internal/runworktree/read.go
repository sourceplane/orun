package runworktree

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LiveRoot returns the directory holding live working trees under an
// object-model root.
func LiveRoot(root string) string { return filepath.Join(root, runDir) }

// LoadLive reads the live snapshot for an execution id, if a working tree
// exists. The bool is false (with a nil error) when there is no live tree.
// This is a read-only accessor for live readers (objread / TUI); it never
// mutates or seals.
func LoadLive(root, execID string) (*Snapshot, bool, error) {
	dir := filepath.Join(LiveRoot(root), sanitizeName(execID))
	snap, err := readSnapshot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("runworktree: read live %s: %w", execID, err)
	}
	return snap, true, nil
}

// ListLive returns the live snapshots of every in-flight working tree under
// root, newest-first by StartedAt. Garbled or partially-written trees are
// skipped. It is read-only.
func ListLive(root string) ([]Snapshot, error) {
	base := LiveRoot(root)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("runworktree: list live: %w", err)
	}
	out := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		snap, err := readSnapshot(filepath.Join(base, e.Name()))
		if err != nil {
			continue // not a complete working tree — skip
		}
		out = append(out, *snap)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ExecutionID < out[j].ExecutionID
	})
	return out, nil
}

// LogPath returns the on-disk path of a step's streamed log within a live
// working tree (for live log tailing). The file may not exist.
func (s *Snapshot) LogPath(root, jobFolder, stepID string) string {
	return filepath.Join(LiveRoot(root), sanitizeName(s.ExecutionID), logsDir, jobFolder, sanitizeName(stepID)+".log")
}
