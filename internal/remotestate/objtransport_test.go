package remotestate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// fakeCloud is a minimal in-memory implementation of the state API object + ref
// planes, enough to drive the HTTP ObjectTransport / RefTransport end-to-end. It
// verifies sha256(body)==digest on object PUT and enforces ref compare-and-swap,
// exactly as the real worker (apps/state-worker) does.
type fakeCloud struct {
	mu      sync.Mutex
	objects map[string][]byte
	refs    map[string]string
	// catalog head digest keyed by environment ("" = project-wide head).
	catalog map[string]string
}

func newFakeCloud() *fakeCloud {
	return &fakeCloud{objects: map[string][]byte{}, refs: map[string]string{}, catalog: map[string]string{}}
}

func (f *fakeCloud) data(w http.ResponseWriter, payload any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": payload})
}

func (f *fakeCloud) apiError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": code}})
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	idx := strings.Index(r.URL.Path, "/state/")
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	suffix := r.URL.Path[idx+len("/state"):] // e.g. "/objects/sha256:..", "/refs/catalogs/current"

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && suffix == "/objects/missing":
		var req ObjectsMissingRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var missing []string
		for _, d := range req.Digests {
			if _, ok := f.objects[d]; !ok {
				missing = append(missing, d)
			}
		}
		f.data(w, ObjectsMissingResponse{Missing: missing})

	case strings.HasPrefix(suffix, "/objects/"):
		digest := strings.TrimPrefix(suffix, "/objects/")
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			sum := sha256.Sum256(body)
			if "sha256:"+hex.EncodeToString(sum[:]) != digest {
				f.apiError(w, http.StatusBadRequest, "digest_mismatch")
				return
			}
			f.objects[digest] = body
			f.data(w, map[string]any{"created": true})
		case http.MethodGet:
			body, ok := f.objects[digest]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body) // raw framed bytes, not enveloped
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	case suffix == "/refs":
		prefix := r.URL.Query().Get("prefix")
		var refs []RefRecord
		for name, target := range f.refs {
			if strings.HasPrefix(name, prefix) {
				refs = append(refs, RefRecord{Name: name, Target: target, Writer: "saas"})
			}
		}
		f.data(w, listRefsResponse{Refs: refs})

	case strings.HasPrefix(suffix, "/refs/"):
		name := strings.TrimPrefix(suffix, "/refs/")
		switch r.Method {
		case http.MethodGet:
			target, ok := f.refs[name]
			if !ok {
				f.apiError(w, http.StatusNotFound, "not_found")
				return
			}
			f.data(w, getRefResponse{Ref: RefRecord{Name: name, Target: target, Writer: "saas"}})
		case http.MethodPut:
			var req updateRefRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			cur, exists := f.refs[name]
			if cur != req.ExpectedTarget && !(req.ExpectedTarget == "" && !exists) {
				f.apiError(w, http.StatusConflict, "ref_conflict")
				return
			}
			f.refs[name] = req.Target
			f.data(w, updateRefResponse{Ref: RefRecord{Name: name, Target: req.Target, Writer: "saas"}})
		case http.MethodDelete:
			delete(f.refs, name)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	case suffix == "/catalog/head" && r.Method == http.MethodPut:
		var req advanceCatalogHeadRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Mirror the worker: the digest must exist in the object plane first.
		if _, ok := f.objects[req.Digest]; !ok {
			f.apiError(w, http.StatusPreconditionFailed, "object_missing")
			return
		}
		prevDigest, had := f.catalog[req.Environment]
		f.catalog[req.Environment] = req.Digest
		var previous *CatalogHeadRecord
		if had {
			previous = &CatalogHeadRecord{Environment: req.Environment, Digest: prevDigest}
		}
		f.data(w, advanceCatalogHeadResponse{
			Head:     CatalogHeadRecord{Environment: req.Environment, Digest: req.Digest, Commit: req.Commit},
			Previous: previous,
		})

	default:
		http.NotFound(w, r)
	}
}

func newCloudClient(t *testing.T) (*Client, *fakeCloud) {
	t.Helper()
	cloud := newFakeCloud()
	srv := httptest.NewServer(cloud)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test", NewStaticTokenSource("sk_test")), cloud
}

func TestRemoteStoresObjectRoundTrip(t *testing.T) {
	ctx := context.Background()
	client, _ := newCloudClient(t)
	store, _ := client.RemoteStores()

	id, err := store.PutBlob(ctx, []byte("hello cloud"))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	kind, body, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if kind != objectstore.KindBlob || string(body) != "hello cloud" {
		t.Fatalf("round-trip = %q/%q", kind, body)
	}
	has, err := store.Has(ctx, id)
	if err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
}

func TestRemoteStoresTreeRoundTrip(t *testing.T) {
	ctx := context.Background()
	client, _ := newCloudClient(t)
	store, _ := client.RemoteStores()

	leaf, err := store.PutBlob(ctx, []byte("leaf"))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	treeID, err := store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "a.txt", Kind: objectstore.KindBlob, ID: leaf},
	})
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	entries, err := store.GetTree(ctx, treeID)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" || entries[0].ID != leaf {
		t.Fatalf("tree entries = %+v", entries)
	}
}

func TestRemoteRefStoreOverHTTP(t *testing.T) {
	ctx := context.Background()
	client, _ := newCloudClient(t)
	store, refs := client.RemoteStores()

	// A ref target should exist as an object first (mirrors the closure-then-ref
	// discipline); the fake doesn't enforce the FK, but use a real id anyway.
	id, _ := store.PutBlob(ctx, []byte("root"))

	// Create from absent.
	if err := refs.Update(ctx, "catalogs/current", "", string(id)); err != nil {
		t.Fatalf("create ref: %v", err)
	}
	got, err := refs.Read(ctx, "catalogs/current")
	if err != nil || got.Target != string(id) {
		t.Fatalf("read ref = %+v, %v", got, err)
	}

	// CAS conflict on a stale old value.
	id2, _ := store.PutBlob(ctx, []byte("root2"))
	if err := refs.Update(ctx, "catalogs/current", string(id2), string(id2)); !errors.Is(err, refstore.ErrConflict) {
		t.Fatalf("stale CAS = %v, want ErrConflict", err)
	}

	// Correct advance.
	if err := refs.Update(ctx, "catalogs/current", string(id), string(id2)); err != nil {
		t.Fatalf("advance: %v", err)
	}

	// Missing ref → ErrNotFound.
	if _, err := refs.Read(ctx, "executions/latest"); !errors.Is(err, refstore.ErrNotFound) {
		t.Fatalf("read missing = %v, want ErrNotFound", err)
	}

	// List by prefix.
	_ = refs.Update(ctx, "sources/main", "", string(id))
	names, err := refs.List(ctx, "sources/")
	if err != nil || len(names) != 1 || names[0] != "sources/main" {
		t.Fatalf("list = %v, %v", names, err)
	}

	// Delete.
	if err := refs.Delete(ctx, "catalogs/current"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := refs.Read(ctx, "catalogs/current"); !errors.Is(err, refstore.ErrNotFound) {
		t.Fatalf("read after delete = %v, want ErrNotFound", err)
	}
}
