package objectstore

import (
	"bytes"
	"context"
	"testing"

	"pgregory.net/rapid"
)

// Invariant 1 (content integrity) + Invariant 3 (idempotent put): an arbitrary
// blob round-trips through put/get, repeat puts return the same id, and the
// returned bytes hash to the id.
func TestProp_BlobRoundTripIdempotent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		data := rapid.SliceOfN(rapid.Byte(), 0, 2048).Draw(rt, "data")
		s := NewMemStore("")
		ctx := context.Background()
		id, err := s.PutBlob(ctx, data)
		if err != nil {
			rt.Fatalf("PutBlob: %v", err)
		}
		id2, err := s.PutBlob(ctx, data)
		if err != nil || id2 != id {
			rt.Fatalf("idempotency: %s vs %s (%v)", id, id2, err)
		}
		kind, body, err := s.Get(ctx, id)
		if err != nil {
			rt.Fatalf("Get: %v", err)
		}
		if kind != KindBlob || !bytes.Equal(body, data) {
			rt.Fatalf("round-trip mismatch")
		}
		// The id must equal the hash of the framed bytes.
		if err := verify(s.Algo(), frame(KindBlob, data), id); err != nil {
			rt.Fatalf("integrity: %v", err)
		}
	})
}

// Content addressing: equal content ⇒ equal id; different content ⇒ different
// id (collision-free in practice for sha256).
func TestProp_ContentAddressing(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a := rapid.SliceOfN(rapid.Byte(), 0, 512).Draw(rt, "a")
		b := rapid.SliceOfN(rapid.Byte(), 0, 512).Draw(rt, "b")
		_, ida, err := computeBlobID(AlgoSHA256, a)
		if err != nil {
			rt.Fatalf("a: %v", err)
		}
		_, idb, err := computeBlobID(AlgoSHA256, b)
		if err != nil {
			rt.Fatalf("b: %v", err)
		}
		if bytes.Equal(a, b) {
			if ida != idb {
				rt.Fatalf("equal content, differing ids")
			}
		} else if ida == idb {
			rt.Fatalf("different content, equal ids (collision)")
		}
	})
}

// Invariant 4 (tree dedup): a tree's id is independent of input entry order,
// and changing a single child id changes the root id.
func TestProp_TreeOrderIndependenceAndSensitivity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		s := NewMemStore("")
		ctx := context.Background()
		// Build N distinct child blobs with distinct names.
		n := rapid.IntRange(1, 6).Draw(rt, "n")
		entries := make([]TreeEntry, 0, n)
		for i := 0; i < n; i++ {
			payload := rapid.SliceOfN(rapid.Byte(), 1, 64).Draw(rt, "payload")
			// Names must be unique + in-alphabet; derive from index.
			name := "e" + string(rune('a'+i))
			id, err := s.PutBlob(ctx, append([]byte(name), payload...))
			if err != nil {
				rt.Fatalf("PutBlob: %v", err)
			}
			entries = append(entries, TreeEntry{Name: name, Kind: KindBlob, ID: id})
		}
		forward, err := s.PutTree(ctx, entries)
		if err != nil {
			rt.Fatalf("PutTree forward: %v", err)
		}
		// Reverse order → same id.
		rev := make([]TreeEntry, len(entries))
		for i := range entries {
			rev[len(entries)-1-i] = entries[i]
		}
		reverse, err := s.PutTree(ctx, rev)
		if err != nil {
			rt.Fatalf("PutTree reverse: %v", err)
		}
		if forward != reverse {
			rt.Fatalf("tree id depends on input order")
		}
		// Mutate one child id → root id must change.
		extra, _ := s.PutBlob(ctx, []byte("mutation"))
		mutated := make([]TreeEntry, len(entries))
		copy(mutated, entries)
		mutated[0].ID = extra
		mutatedID, err := s.PutTree(ctx, mutated)
		if err != nil {
			rt.Fatalf("PutTree mutated: %v", err)
		}
		if mutatedID == forward && extra != entries[0].ID {
			rt.Fatalf("tree id insensitive to child change")
		}
	})
}
