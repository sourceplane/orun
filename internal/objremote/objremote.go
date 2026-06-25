// Package objremote implements object substitution between two object-model
// endpoints (specs/orun-object-model remote-and-consumers.md §1). Because every
// object is named by the hash of its content, syncing is a set difference, not a
// replication protocol: copy the objects the destination lacks for a ref's
// reachable closure, then move the destination ref. A "remote" is simply a
// second object + ref store at a different root (the file:// reference driver);
// an R2/S3 driver is a thin adapter over the same interfaces.
package objremote

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"golang.org/x/sync/errgroup"
)

// uploadConcurrency bounds how many objects are copied to the destination at
// once. Uploads are independent and idempotent, so the only reason to cap them
// is to avoid opening an unbounded number of connections for a large delta; 8 is
// enough to hide per-request latency without flooding the host.
const uploadConcurrency = 8

// Endpoint bundles an object store with its ref store.
type Endpoint struct {
	Objects objectstore.ObjectStore
	Refs    refstore.RefStore
}

// Result reports the work a sync performed.
type Result struct {
	Closure  int  // objects in the ref's closure
	Copied   int  // objects the destination lacked and received
	Skipped  int  // objects already present at the destination
	RefMoved bool // whether the destination ref was advanced
}

// Sync copies the closure reachable from from's ref into to, then advances to's
// ref to the same target. Push and Pull are Sync in the two directions.
func Sync(ctx context.Context, from, to Endpoint, refName string) (Result, error) {
	var res Result
	ref, err := from.Refs.Read(ctx, refName)
	if err != nil {
		return res, fmt.Errorf("objremote: read source ref %q: %w", refName, err)
	}
	target := objectstore.ObjectID(ref.Target)

	// Collect the closure from the source (a local walk: cheap disk reads).
	var ids []objectstore.ObjectID
	if err := from.Objects.Walk(ctx, target, func(id objectstore.ObjectID, _ objectstore.Kind) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		return res, fmt.Errorf("objremote: walk %s: %w", target, err)
	}
	res.Closure = len(ids)

	// Fast path: the destination ref already names this target. Its closure was
	// fully copied before the ref advanced (moveRef runs last, below), so every
	// object is guaranteed present — skip the network presence scan entirely.
	// This is what makes an unchanged re-push near-instant.
	if cur, err := to.Refs.Read(ctx, refName); err == nil && cur.Target == ref.Target {
		res.Skipped = len(ids)
		return res, nil
	}

	// Resolve the destination's absent subset. A batch-capable destination (the
	// hosted object plane) answers in one round-trip; a plain store falls back to
	// a per-object Has scan inside missingObjects.
	missing, err := missingObjects(ctx, to.Objects, ids)
	if err != nil {
		return res, err
	}
	res.Skipped = len(ids) - len(missing)

	// Copy what the destination lacks, in parallel. Each object is
	// content-addressed and its put is idempotent, so the copies are independent
	// and order-free; the ref is not advanced until every copy lands, so a
	// partial failure never leaves a reachable-but-incomplete closure. Bounded by
	// uploadConcurrency. On the first error the group cancels the rest and Sync
	// reports however many copies had already completed.
	if len(missing) > 0 {
		var copied atomic.Int64
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(uploadConcurrency)
		for _, id := range missing {
			g.Go(func() error {
				if err := copyObject(gctx, from.Objects, to.Objects, id); err != nil {
					return err
				}
				copied.Add(1)
				return nil
			})
		}
		err := g.Wait()
		res.Copied = int(copied.Load())
		if err != nil {
			return res, err
		}
	}

	// Advance the destination ref (CAS, bounded retry on a lost race). A no-op
	// when it already points at the target.
	moved, err := moveRef(ctx, to.Refs, refName, ref.Target)
	if err != nil {
		return res, err
	}
	res.RefMoved = moved
	return res, nil
}

// missingObjects returns the subset of ids absent from dst. A destination that
// implements objectstore.MissingFilter (the hosted object plane) answers the
// whole closure in one batched round-trip; any other store is probed per object
// with Has — cheap for a local file:// destination, where a "round-trip" is a
// disk stat.
func missingObjects(ctx context.Context, dst objectstore.ObjectStore, ids []objectstore.ObjectID) ([]objectstore.ObjectID, error) {
	if bf, ok := dst.(objectstore.MissingFilter); ok {
		missing, err := bf.MissingObjects(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("objremote: missing scan: %w", err)
		}
		return missing, nil
	}
	var missing []objectstore.ObjectID
	for _, id := range ids {
		has, err := dst.Has(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("objremote: dest has %s: %w", id, err)
		}
		if !has {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// Push copies local's ref closure to the remote and advances the remote ref.
func Push(ctx context.Context, local, remote Endpoint, refName string) (Result, error) {
	return Sync(ctx, local, remote, refName)
}

// Pull copies the remote's ref closure to local and advances the local ref.
func Pull(ctx context.Context, local, remote Endpoint, refName string) (Result, error) {
	return Sync(ctx, remote, local, refName)
}

// copyObject re-stores a single object in dst, preserving its content id.
func copyObject(ctx context.Context, src, dst objectstore.ObjectStore, id objectstore.ObjectID) error {
	kind, body, err := src.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("objremote: get %s: %w", id, err)
	}
	if kind == objectstore.KindBlob {
		got, err := dst.PutBlob(ctx, body)
		if err != nil {
			return fmt.Errorf("objremote: put blob %s: %w", id, err)
		}
		return verifyCopied(id, got)
	}
	entries, err := src.GetTree(ctx, id)
	if err != nil {
		return fmt.Errorf("objremote: get tree %s: %w", id, err)
	}
	got, err := dst.PutTree(ctx, entries)
	if err != nil {
		return fmt.Errorf("objremote: put tree %s: %w", id, err)
	}
	return verifyCopied(id, got)
}

// verifyCopied guards against a destination that hashes differently (e.g. a
// different algo); content addressing means the id must be preserved.
func verifyCopied(want, got objectstore.ObjectID) error {
	if want != got {
		return fmt.Errorf("objremote: copy id mismatch: src %s, dst %s", want, got)
	}
	return nil
}

func moveRef(ctx context.Context, refs refstore.RefStore, name, target string) (bool, error) {
	const maxAttempts = 8
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cur := ""
		if r, err := refs.Read(ctx, name); err == nil {
			cur = r.Target
		} else if !errors.Is(err, refstore.ErrNotFound) {
			return false, fmt.Errorf("objremote: read dest ref %q: %w", name, err)
		}
		if cur == target {
			return false, nil
		}
		err := refs.Update(ctx, name, cur, target)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, refstore.ErrConflict) {
			continue
		}
		return false, fmt.Errorf("objremote: move dest ref %q: %w", name, err)
	}
	return false, fmt.Errorf("objremote: move dest ref %q: too many conflicts", name)
}
