package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func TestLiteracyEmbedded(t *testing.T) {
	body := string(Literacy())
	if len(body) == 0 {
		t.Fatal("embedded literacy empty")
	}
	// The invariants the whole design leans on must be stated.
	for _, want := range []string{
		"Lifecycle is derived",
		"status-write tool",
		"blast radius",
		"secret://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("literacy missing %q", want)
		}
	}
}

func TestSealLiteracyIdempotent(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	id1, err := SealLiteracy(ctx, store, refs)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pure, err := LiteracyID(objectstore.AlgoSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != pure {
		t.Fatalf("sealed id %s != pure id %s", id1, pure)
	}
	id2, err := SealLiteracy(ctx, store, refs)
	if err != nil || id2 != id1 {
		t.Fatalf("re-seal not idempotent: %s vs %s (%v)", id2, id1, err)
	}
	ref, err := refs.Read(ctx, LiteracyRef(LiteracyVersion))
	if err != nil || ref.Target != string(id1) {
		t.Fatalf("ref %v (%v), want %s", ref, err, id1)
	}
}
