// Package objgc is the reachability garbage collector for the object-model store
// (specs/orun-object-model object-store.md §7). It runs in two phases:
//
//  1. Retention — optionally prune the per-execution refs (executions/by-id/*)
//     for all but the newest N executions, so those executions become
//     unreachable.
//  2. Mark-and-sweep — mark the closure reachable from the surviving refs and
//     delete every object that is neither marked nor inside the grace window
//     (objects written within GracePeriod are never swept, so an in-flight seal
//     whose ref has not moved yet is safe).
//
// GC is safe to interrupt: it deletes only proven-unreachable objects, so a
// partial run leaves a valid store.
package objgc

import (
	"context"
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objindex"
)

const execByIDPrefix = "executions/by-id/"

// ager is the optional capability a store exposes for the grace window. The
// local driver satisfies it via file mtime; a store that does not is GC'd
// without grace.
type ager interface {
	ModTime(ctx context.Context, id objectstore.ObjectID) (time.Time, error)
}

// Options tune a collection run.
type Options struct {
	// KeepExecutions retains this many newest executions (by start time); the
	// per-execution refs of the rest are pruned so their closures can be swept.
	// Zero keeps all executions.
	KeepExecutions int
	// GracePeriod protects objects written within this window from sweeping.
	// Zero disables grace (every unreachable object is eligible).
	GracePeriod time.Duration
	// DryRun reports what would be removed without deleting anything.
	DryRun bool
	// Now overrides the clock for the grace window (defaults to time.Now).
	Now time.Time
}

// Result summarizes a collection run.
type Result struct {
	Scanned        int
	Marked         int
	PrunedExecRefs int
	Swept          int
	Skipped        int // unreachable but within the grace window
	DryRun         bool
}

// Collect runs retention + mark-and-sweep over store using refs as the GC roots.
// ix (may be nil) supplies the newest-first execution ordering for retention.
func Collect(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, ix *objindex.Indexer, opts Options) (Result, error) {
	res := Result{DryRun: opts.DryRun}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	// Phase 1 — retention: prune by-id refs of the oldest executions.
	if opts.KeepExecutions > 0 && ix != nil {
		pruned, err := pruneExecutions(ctx, refs, ix, opts.KeepExecutions, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.PrunedExecRefs = pruned
	}

	// Phase 2a — mark the closure reachable from every surviving ref.
	marked := map[objectstore.ObjectID]struct{}{}
	names, err := refs.List(ctx, "")
	if err != nil {
		return res, fmt.Errorf("objgc: list refs: %w", err)
	}
	for _, name := range names {
		ref, err := refs.Read(ctx, name)
		if err != nil {
			return res, fmt.Errorf("objgc: read ref %q: %w", name, err)
		}
		target := objectstore.ObjectID(ref.Target)
		if has, _ := store.Has(ctx, target); !has {
			continue // dangling ref target; nothing to mark
		}
		if err := store.Walk(ctx, target, func(id objectstore.ObjectID, _ objectstore.Kind) error {
			marked[id] = struct{}{}
			return nil
		}); err != nil {
			return res, fmt.Errorf("objgc: walk %q: %w", name, err)
		}
	}
	res.Marked = len(marked)

	// Phase 2b — sweep unreachable objects outside the grace window.
	ag, hasAger := store.(ager)
	err = store.Iterate(ctx, func(id objectstore.ObjectID) error {
		res.Scanned++
		if _, ok := marked[id]; ok {
			return nil
		}
		if opts.GracePeriod > 0 && hasAger {
			if mt, merr := ag.ModTime(ctx, id); merr == nil && now.Sub(mt) < opts.GracePeriod {
				res.Skipped++
				return nil
			}
		}
		if opts.DryRun {
			res.Swept++
			return nil
		}
		if err := store.Delete(ctx, id); err != nil {
			return fmt.Errorf("objgc: delete %s: %w", id, err)
		}
		res.Swept++
		return nil
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// pruneExecutions deletes the executions/by-id refs of all but the newest keep
// executions, returning the number pruned.
func pruneExecutions(ctx context.Context, refs refstore.RefStore, ix *objindex.Indexer, keep int, dryRun bool) (int, error) {
	entries, err := ix.BuildExecutions(ctx) // newest-first
	if err != nil {
		return 0, fmt.Errorf("objgc: list executions: %w", err)
	}
	if len(entries) <= keep {
		return 0, nil
	}
	// Object ids to keep (newest `keep`).
	keepSet := map[string]struct{}{}
	for _, e := range entries[:keep] {
		keepSet[e.ObjectID] = struct{}{}
	}
	names, err := refs.List(ctx, execByIDPrefix)
	if err != nil {
		return 0, fmt.Errorf("objgc: list execution refs: %w", err)
	}
	pruned := 0
	for _, name := range names {
		ref, err := refs.Read(ctx, name)
		if err != nil {
			return pruned, fmt.Errorf("objgc: read ref %q: %w", name, err)
		}
		if _, ok := keepSet[ref.Target]; ok {
			continue
		}
		if !dryRun {
			if err := refs.Delete(ctx, name); err != nil {
				return pruned, fmt.Errorf("objgc: delete ref %q: %w", name, err)
			}
		}
		pruned++
	}
	return pruned, nil
}
