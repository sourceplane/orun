package objectstore

import (
	"context"
	"errors"
	"testing"
)

// fakeObjectTransport is an in-memory ObjectTransport: a digest → framed-bytes
// map that verifies hash(bytes)==digest on Put, exactly as the hosted plane
// must. It lets the RemoteStore be exercised without any HTTP.
type fakeObjectTransport struct {
	algo Algo
	objs map[string][]byte
	puts int
}

func newFakeObjectTransport() *fakeObjectTransport {
	return &fakeObjectTransport{algo: DefaultAlgo, objs: map[string][]byte{}}
}

func (f *fakeObjectTransport) HasObject(_ context.Context, digest string) (bool, error) {
	_, ok := f.objs[digest]
	return ok, nil
}

func (f *fakeObjectTransport) GetObject(_ context.Context, digest string) ([]byte, bool, error) {
	b, ok := f.objs[digest]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), b...), true, nil
}

func (f *fakeObjectTransport) PutObject(_ context.Context, digest, _ string, framed []byte) error {
	// The server verifies the body hashes to the claimed digest.
	if err := verify(f.algo, framed, ObjectID(digest)); err != nil {
		return err
	}
	if _, ok := f.objs[digest]; !ok {
		f.objs[digest] = append([]byte(nil), framed...)
	}
	f.puts++
	return nil
}

func TestRemoteStoreBlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteStore(newFakeObjectTransport(), "", "")

	id, err := r.PutBlob(ctx, []byte("hello world"))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	kind, body, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if kind != KindBlob || string(body) != "hello world" {
		t.Fatalf("round-trip = %q/%q", kind, body)
	}
	has, err := r.Has(ctx, id)
	if err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	missing, err := r.Has(ctx, ObjectID("sha256:"+"00000000000000000000000000000000000000000000000000000000000000ff"))
	if err != nil || missing {
		t.Fatalf("Has(absent) = %v, %v", missing, err)
	}
}

func TestRemoteStoreIDsMatchLocal(t *testing.T) {
	ctx := context.Background()
	mem := NewMemStore("")
	r := NewRemoteStore(newFakeObjectTransport(), "", "")

	// The same content must yield the same id in both stores — the property that
	// makes "the App pushed it" and "the CLI pushed it" the same object.
	for _, data := range [][]byte{[]byte(""), []byte("a"), []byte(`{"plan":"A"}`)} {
		local, err := mem.PutBlob(ctx, data)
		if err != nil {
			t.Fatalf("mem PutBlob: %v", err)
		}
		remote, err := r.PutBlob(ctx, data)
		if err != nil {
			t.Fatalf("remote PutBlob: %v", err)
		}
		if local != remote {
			t.Fatalf("id mismatch for %q: local %s, remote %s", data, local, remote)
		}
	}
}

func TestRemoteStoreTreeRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteStore(newFakeObjectTransport(), "", "")

	leaf, err := r.PutBlob(ctx, []byte("leaf"))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	treeID, err := r.PutTree(ctx, []TreeEntry{
		{Name: "b.txt", Kind: KindBlob, ID: leaf},
		{Name: "a.txt", Kind: KindBlob, ID: leaf},
	})
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	entries, err := r.GetTree(ctx, treeID)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	// Entries come back sorted by name.
	if len(entries) != 2 || entries[0].Name != "a.txt" || entries[1].Name != "b.txt" {
		t.Fatalf("tree entries = %+v", entries)
	}
	// GetTree on a blob is ErrInvalid.
	if _, err := r.GetTree(ctx, leaf); !errors.Is(err, ErrInvalid) {
		t.Fatalf("GetTree(blob) = %v, want ErrInvalid", err)
	}
}

func TestRemoteStoreWalk(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteStore(newFakeObjectTransport(), "", "")

	leaf, _ := r.PutBlob(ctx, []byte("leaf"))
	sub, _ := r.PutTree(ctx, []TreeEntry{{Name: "leaf", Kind: KindBlob, ID: leaf}})
	root, _ := r.PutTree(ctx, []TreeEntry{{Name: "sub", Kind: KindTree, ID: sub}})

	seen := map[ObjectID]Kind{}
	if err := r.Walk(ctx, root, func(id ObjectID, k Kind) error { seen[id] = k; return nil }); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(seen) != 3 || seen[root] != KindTree || seen[sub] != KindTree || seen[leaf] != KindBlob {
		t.Fatalf("walk closure = %+v", seen)
	}
}

func TestRemoteStoreGetMissing(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteStore(newFakeObjectTransport(), "", "")
	_, _, err := r.Get(ctx, ObjectID("sha256:"+"00000000000000000000000000000000000000000000000000000000000000ab"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(absent) = %v, want ErrNotFound", err)
	}
}

func TestRemoteStoreGetCorrupt(t *testing.T) {
	ctx := context.Background()
	ft := newFakeObjectTransport()
	r := NewRemoteStore(ft, "", "")
	id, _ := r.PutBlob(ctx, []byte("real"))
	// Tamper with the stored bytes behind the store's back.
	ft.objs[string(id)] = frame(KindBlob, []byte("tampered"))
	if _, _, err := r.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get(tampered) = %v, want ErrCorrupt", err)
	}
}

func TestRemoteStoreUnsupported(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteStore(newFakeObjectTransport(), "", "")
	if err := r.Iterate(ctx, func(ObjectID) error { return nil }); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Iterate = %v, want ErrUnsupported", err)
	}
	if err := r.Delete(ctx, ObjectID("sha256:ab")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Delete = %v, want ErrUnsupported", err)
	}
}
