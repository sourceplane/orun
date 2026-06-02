// Package workingview provides the L4 inspection surface for the object-model
// store (specs/orun-object-model design.md §2.5, cli-surface.md §2): integrity
// checking (fsck), a materialized human-readable checkout, and the read
// primitives the `orun objects` porcelain is built on. Everything here is
// derived from L0+L2 and can be regenerated at any time.
package workingview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// Problem is a single fsck finding.
type Problem struct {
	Kind   string // "corrupt" | "ref-missing" | "dangling"
	Target string // object id or ref name
	Detail string
}

func (p Problem) String() string {
	return fmt.Sprintf("%s: %s (%s)", p.Kind, p.Target, p.Detail)
}

// Fsck verifies store + ref integrity: every stored object must hash to its id,
// and every ref target's reachable closure must be fully present. It returns the
// list of problems (empty = healthy) rather than failing on the first, so a
// single run reports everything wrong.
func Fsck(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore) ([]Problem, error) {
	var problems []Problem

	// 1. Content integrity — Get re-hashes and returns ErrCorrupt on mismatch.
	if err := store.Iterate(ctx, func(id objectstore.ObjectID) error {
		if _, _, err := store.Get(ctx, id); err != nil {
			if errors.Is(err, objectstore.ErrCorrupt) {
				problems = append(problems, Problem{Kind: "corrupt", Target: string(id), Detail: "bytes do not hash to id"})
				return nil
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("fsck: iterate: %w", err)
	}

	// 2. Ref closure completeness.
	names, err := refs.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fsck: list refs: %w", err)
	}
	for _, name := range names {
		ref, err := refs.Read(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("fsck: read ref %q: %w", name, err)
		}
		target := objectstore.ObjectID(ref.Target)
		has, err := store.Has(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("fsck: has %q: %w", target, err)
		}
		if !has {
			problems = append(problems, Problem{Kind: "ref-missing", Target: name, Detail: "ref target " + ref.Target + " absent"})
			continue
		}
		if err := store.Walk(ctx, target, func(objectstore.ObjectID, objectstore.Kind) error { return nil }); err != nil {
			if errors.Is(err, objectstore.ErrNotFound) {
				problems = append(problems, Problem{Kind: "dangling", Target: name, Detail: "closure of " + ref.Target + " has a missing object"})
				continue
			}
			if errors.Is(err, objectstore.ErrCorrupt) {
				continue // already reported by the integrity pass
			}
			return nil, fmt.Errorf("fsck: walk %q: %w", name, err)
		}
	}
	return problems, nil
}

// ResolveRef resolves a porcelain argument to an object id: a literal
// "<algo>:<hex>" id is returned as-is; otherwise arg is treated as a ref name.
func ResolveRef(ctx context.Context, refs refstore.RefStore, arg string) (objectstore.ObjectID, error) {
	if objectstore.ValidateID(objectstore.ObjectID(arg)) == nil {
		return objectstore.ObjectID(arg), nil
	}
	ref, err := refs.Read(ctx, arg)
	if err != nil {
		return "", err
	}
	return objectstore.ObjectID(ref.Target), nil
}

// CatObject returns an object's body for display. JSON blobs are re-indented for
// readability; non-JSON blobs and trees are returned verbatim (trees as their
// canonical body).
func CatObject(ctx context.Context, store objectstore.ObjectStore, id objectstore.ObjectID) ([]byte, error) {
	kind, body, err := store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if kind == objectstore.KindBlob {
		var pretty interface{}
		if json.Unmarshal(body, &pretty) == nil {
			if out, err := json.MarshalIndent(pretty, "", "  "); err == nil {
				return out, nil
			}
		}
	}
	return body, nil
}

// LsTree returns the entries of a tree object.
func LsTree(ctx context.Context, store objectstore.ObjectStore, id objectstore.ObjectID) ([]objectstore.TreeEntry, error) {
	return store.GetTree(ctx, id)
}

// Materialize writes a human-readable checkout of the object at root into dest:
// a tree becomes a directory and a blob becomes a file (JSON re-indented). It is
// idempotent — dest is created if absent. The materialized tree is a cache; it
// may be deleted and rebuilt at any time.
func Materialize(ctx context.Context, store objectstore.ObjectStore, root objectstore.ObjectID, dest string) error {
	kind, body, err := store.Get(ctx, root)
	if err != nil {
		return err
	}
	if kind == objectstore.KindBlob {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, prettyIfJSON(body), 0o644)
	}
	// Tree → directory.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	entries, err := store.GetTree(ctx, root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := Materialize(ctx, store, e.ID, filepath.Join(dest, e.Name)); err != nil {
			return err
		}
	}
	return nil
}

func prettyIfJSON(body []byte) []byte {
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		if out, err := json.MarshalIndent(v, "", "  "); err == nil {
			return append(out, '\n')
		}
	}
	return body
}
