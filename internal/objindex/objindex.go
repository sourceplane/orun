// Package objindex builds and reads the L3 derived indexes over the
// object-model graph (specs/orun-object-model design.md §2.4). Indexes are
// rebuildable caches under <root>/index/ — never authoritative; every read
// falls back to a graph walk on a miss, and Reindex reproduces them
// deterministically from refs + objects.
//
// This milestone (M8) implements the executions index: a newest-first listing
// of every sealed execution, enumerated from the executions/by-id/ refs that
// execseal publishes.
package objindex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// execByIDPrefix matches the per-execution ref namespace execseal publishes.
const execByIDPrefix = "executions/by-id/"

// ExecEntry is one row of the executions index.
type ExecEntry struct {
	ExecutionID string            `json:"executionId"`
	ObjectID    string            `json:"objectId"`
	RevisionID  string            `json:"revisionId"`
	TriggerID   string            `json:"triggerId,omitempty"`
	Status      string            `json:"status"`
	StartedAt   string            `json:"startedAt,omitempty"`
	FinishedAt  string            `json:"finishedAt,omitempty"`
	Summary     nodes.ExecSummary `json:"summary"`
}

// Indexer reads and rebuilds indexes over a store + ref store.
type Indexer struct {
	store objectstore.ObjectStore
	refs  refstore.RefStore
	dir   string
}

// New constructs an Indexer rooted at <root>/index.
func New(store objectstore.ObjectStore, refs refstore.RefStore, root string) *Indexer {
	return &Indexer{store: store, refs: refs, dir: filepath.Join(root, "index")}
}

// BuildExecutions enumerates every sealed execution from the executions/by-id/
// refs and returns the entries newest-first (by StartedAt desc, then
// ExecutionID). This is the authoritative walk; ListExecutions caches it.
func (ix *Indexer) BuildExecutions(ctx context.Context) ([]ExecEntry, error) {
	names, err := ix.refs.List(ctx, execByIDPrefix)
	if err != nil {
		return nil, fmt.Errorf("objindex: list execution refs: %w", err)
	}
	entries := make([]ExecEntry, 0, len(names))
	for _, name := range names {
		ref, err := ix.refs.Read(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("objindex: read ref %q: %w", name, err)
		}
		exec, err := ix.readExecution(ctx, objectstore.ObjectID(ref.Target))
		if err != nil {
			return nil, fmt.Errorf("objindex: read execution %q: %w", ref.Target, err)
		}
		entries = append(entries, ExecEntry{
			ExecutionID: exec.ExecutionID,
			ObjectID:    ref.Target,
			RevisionID:  exec.RevisionID,
			TriggerID:   exec.TriggerID,
			Status:      exec.Status,
			StartedAt:   formatTime(exec.StartedAt),
			FinishedAt:  formatTimePtr(exec.FinishedAt),
			Summary:     exec.Summary,
		})
	}
	sortExecEntries(entries)
	return entries, nil
}

// readExecution decodes the execution.json blob inside a sealed execution tree.
func (ix *Indexer) readExecution(ctx context.Context, id objectstore.ObjectID) (nodes.ExecutionRun, error) {
	entries, err := ix.store.GetTree(ctx, id)
	if err != nil {
		return nodes.ExecutionRun{}, err
	}
	for _, e := range entries {
		if e.Name == "execution.json" {
			_, body, err := ix.store.Get(ctx, e.ID)
			if err != nil {
				return nodes.ExecutionRun{}, err
			}
			return nodes.Decode[nodes.ExecutionRun](body)
		}
	}
	return nodes.ExecutionRun{}, fmt.Errorf("%w: execution.json missing in %s", objectstore.ErrInvalid, id)
}

// allExecutionsPath is the cached executions index file.
func (ix *Indexer) allExecutionsPath() string {
	return filepath.Join(ix.dir, "executions", "all.json")
}

// Reindex rebuilds and writes the executions index. Deterministic: identical
// graphs produce byte-identical index files.
func (ix *Indexer) Reindex(ctx context.Context) error {
	entries, err := ix.BuildExecutions(ctx)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("objindex: marshal: %w", err)
	}
	return writeAtomic(ix.allExecutionsPath(), data)
}

// ListExecutions returns executions newest-first, reading the cached index when
// present and falling back to a fresh BuildExecutions walk on a miss.
func (ix *Indexer) ListExecutions(ctx context.Context) ([]ExecEntry, error) {
	data, err := os.ReadFile(ix.allExecutionsPath())
	if err != nil {
		return ix.BuildExecutions(ctx) // miss → authoritative walk
	}
	var entries []ExecEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return ix.BuildExecutions(ctx) // corrupt cache → walk
	}
	return entries, nil
}

func sortExecEntries(entries []ExecEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].StartedAt != entries[j].StartedAt {
			return entries[i].StartedAt > entries[j].StartedAt // newest first
		}
		return entries[i].ExecutionID > entries[j].ExecutionID
	})
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("objindex: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("objindex: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("objindex: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("objindex: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("objindex: rename: %w", err)
	}
	return nil
}
