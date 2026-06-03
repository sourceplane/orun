package runworktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// RecoveryResult records the outcome of recovering one stale working tree.
type RecoveryResult struct {
	ExecutionID string
	Status      string // terminal status it was sealed as
	SealedID    objectstore.ObjectID
	WasComplete bool // true if the run had already reached a terminal status pre-crash
}

// RecoverStale scans for working trees whose heartbeat has gone stale (the
// owning process crashed) and seals each one. A tree that had already reached a
// terminal status before the crash is sealed as that status (an idempotent
// finish of a run that crashed after sealing-objects but before cleanup); one
// that crashed mid-run is sealed as failed. Fresh (still-heartbeating) trees are
// left untouched. It is safe to call at the start of any orun invocation.
func (m *Manager) RecoverStale(ctx context.Context) ([]RecoveryResult, error) {
	base := filepath.Join(m.root, runDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("runworktree: scan run dir: %w", err)
	}
	// Deterministic order so recovery is reproducible.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var results []RecoveryResult
	for _, name := range names {
		dir := filepath.Join(base, name)
		lock, err := readLock(dir)
		if err != nil {
			continue // not a working tree (no/garbled lock) — leave it
		}
		if m.clk.Now().Sub(lock.LastHeartbeat) < m.staleAfter {
			continue // still alive
		}
		res, err := m.recoverOne(ctx, dir)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// recoverOne seals a single stale working tree and clears its handle + dir.
func (m *Manager) recoverOne(ctx context.Context, dir string) (RecoveryResult, error) {
	snap, err := readSnapshot(dir)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("runworktree: read snapshot %s: %w", dir, err)
	}
	complete := nodes.IsTerminalStatus(snap.Status)
	status := snap.Status
	if !complete {
		status = nodes.StatusFailed
		snap.Status = status
		if snap.FinishedAt == nil {
			snap.FinishedAt = ptr(m.clk.Now())
		}
	}
	id, err := m.sealSnapshot(ctx, dir, snap)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("runworktree: seal recovered %s: %w", snap.ExecutionID, err)
	}
	_ = m.refs.Delete(ctx, liveRefName(snap.ExecutionID))
	_ = os.RemoveAll(dir)
	return RecoveryResult{
		ExecutionID: snap.ExecutionID,
		Status:      status,
		SealedID:    id,
		WasComplete: complete,
	}, nil
}
