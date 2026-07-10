package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// The base literacy is the versioned, binary-shipped layer of orun
// understanding every agent type extends (specs/orun-agents/data-model.md §4).
// It is embedded so an agent's understanding of orun tracks the orun version
// it runs inside: upgrade the binary and the literacy upgrades with it;
// personas never restate orun mechanics and never rot.

//go:embed literacy.md
var literacyBody []byte

// LiteracyName is the well-known name agent types extend by default.
const LiteracyName = "base-orun-literacy"

// LiteracyVersion identifies this revision of the embedded literacy. Bump it
// whenever literacy.md changes in a way agents should be able to distinguish;
// the content hash is the real identity, this is the human-readable rung.
const LiteracyVersion = "v2"

// Literacy returns the embedded base-literacy document.
func Literacy() []byte { return literacyBody }

// LiteracyRef is the ref that pins a sealed literacy version in a store.
func LiteracyRef(version string) string { return "agents/literacy/" + version }

// LiteracyID returns the content id of the embedded literacy without writing.
func LiteracyID(algo objectstore.Algo) (objectstore.ObjectID, error) {
	return objectstore.HashBlob(algo, literacyBody)
}

// SealLiteracy writes the embedded literacy into the store as a
// content-addressed blob and points refs/agents/literacy/<version> at it.
// Idempotent: re-sealing an unchanged literacy is a no-op ref move.
func SealLiteracy(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore) (objectstore.ObjectID, error) {
	id, err := store.PutBlob(ctx, literacyBody)
	if err != nil {
		return "", fmt.Errorf("agent: seal literacy: %w", err)
	}
	name := LiteracyRef(LiteracyVersion)
	cur, err := refs.Read(ctx, name)
	switch {
	case err == nil:
		if cur.Target == string(id) {
			return id, nil
		}
		if err := refs.Update(ctx, name, cur.Target, string(id)); err != nil {
			return "", fmt.Errorf("agent: move literacy ref: %w", err)
		}
	case errors.Is(err, refstore.ErrNotFound):
		if err := refs.Update(ctx, name, "", string(id)); err != nil {
			return "", fmt.Errorf("agent: create literacy ref: %w", err)
		}
	default:
		return "", fmt.Errorf("agent: read literacy ref: %w", err)
	}
	return id, nil
}
