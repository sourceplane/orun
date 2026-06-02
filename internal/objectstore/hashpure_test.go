package objectstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHashBlobMatchesPutBlob(t *testing.T) {
	t.Parallel()
	s := NewMemStore("")
	data := []byte("content addressed")
	want, err := s.PutBlob(context.Background(), data)
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	got, err := HashBlob(AlgoSHA256, data)
	if err != nil {
		t.Fatalf("HashBlob: %v", err)
	}
	if got != want {
		t.Fatalf("HashBlob = %s, PutBlob = %s", got, want)
	}
	if _, err := HashBlob(Algo("md5"), data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("HashBlob(bad algo) = %v, want ErrInvalid", err)
	}
}

func TestHashTreeMatchesPutTree(t *testing.T) {
	t.Parallel()
	s := NewMemStore("")
	ctx := context.Background()
	child, _ := s.PutBlob(ctx, []byte("x"))
	entries := []TreeEntry{{Name: "a", Kind: KindBlob, ID: child}}
	want, err := s.PutTree(ctx, entries)
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	got, err := HashTree(AlgoSHA256, entries)
	if err != nil {
		t.Fatalf("HashTree: %v", err)
	}
	if got != want {
		t.Fatalf("HashTree = %s, PutTree = %s", got, want)
	}
	// Invalid entry surfaces ErrInvalid without persisting.
	bad := []TreeEntry{{Name: "a/b", Kind: KindBlob, ID: ObjectID("sha256:" + strings.Repeat("a", 64))}}
	if _, err := HashTree(AlgoSHA256, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("HashTree(bad) = %v, want ErrInvalid", err)
	}
}
