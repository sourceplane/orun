package revision

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// newWriterCfg returns a Config wired against store with deterministic Now
// and NewID stubs, so on-disk JSON is byte-stable across runs.
func newWriterCfg(store statestore.StateStore, fixedNow time.Time) Config {
	return Config{
		Store: store,
		Now:   func() time.Time { return fixedNow },
		NewID: func() string { return "rev_01HZTESTDETERMINISTIC0001" },
	}
}

// readVersionDoc decodes .orun/version.json from a workspace root.
func readVersionDoc(t *testing.T, store statestore.StateStore) StateStoreVersion {
	t.Helper()
	raw, _, err := store.Read(context.Background(), "version.json")
	if err != nil {
		t.Fatalf("read version.json: %v", err)
	}
	var v StateStoreVersion
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode version.json: %v", err)
	}
	return v
}

// rootOf is a small reflection-free way to recover the local store root for
// AssertJSONFile-style on-disk checks.
func rootOf(store statestore.StateStore) string {
	type rooted interface{ Root() string }
	if r, ok := store.(rooted); ok {
		return r.Root()
	}
	return ""
}

func TestWriteRevision_Golden(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	cfg := newWriterCfg(store, now)

	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"

	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}

	// Returned revision is well-formed.
	if err := ValidateRevisionKey(rev.RevisionKey); err != nil {
		t.Fatalf("invalid revision key %q: %v", rev.RevisionKey, err)
	}
	if rev.PlanShortHash != "feedface" {
		t.Errorf("PlanShortHash=%q want feedface", rev.PlanShortHash)
	}
	if rev.RevisionID != "rev_01HZTESTDETERMINISTIC0001" {
		t.Errorf("RevisionID = %q", rev.RevisionID)
	}
	if rev.Summary.Scope != triggerctx.PlanScopeFull {
		t.Errorf("Summary.Scope=%q", rev.Summary.Scope)
	}

	// Files exist on disk under the expected paths.
	root := rootOf(store)
	if root == "" {
		t.Fatal("could not recover store root")
	}
	for _, rel := range []string{
		filepath.Join("revisions", rev.RevisionKey, "trigger.json"),
		filepath.Join("revisions", rev.RevisionKey, "revision.json"),
		filepath.Join("revisions", rev.RevisionKey, "plan.json"),
		filepath.Join("refs", "latest-revision.json"),
		filepath.Join("refs", "triggers", trig.TriggerName, "latest.json"),
		filepath.Join("indexes", "revisions", rev.RevisionKey+".json"),
		"version.json",
	} {
		raw, _, err := store.Read(context.Background(), strings.ReplaceAll(rel, string(filepath.Separator), "/"))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
		if len(raw) == 0 {
			t.Errorf("empty %s", rel)
		}
	}

	// plan.json bytes are unmodified (the planHash invariant).
	gotPlan, _, err := store.Read(context.Background(), statestore.PlanPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if string(gotPlan) != string(plan) {
		t.Errorf("plan.json bytes mutated by writer\n got=%q\nwant=%q", gotPlan, plan)
	}

	// indexes/revisions/<key>.json was finalized (no longer the {"reserved":true} stub).
	idxRaw, _, err := store.Read(context.Background(), statestore.RevisionIndexPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if strings.Contains(string(idxRaw), "reserved") {
		t.Errorf("index entry still holds reservation: %s", idxRaw)
	}
	var entry statestore.RevisionIndexEntry
	if err := json.Unmarshal(idxRaw, &entry); err != nil {
		t.Fatalf("decode index entry: %v", err)
	}
	if entry.RevisionID != rev.RevisionID || entry.RevisionKey != rev.RevisionKey {
		t.Errorf("index entry mismatch: %+v", entry)
	}

	// refs/latest-revision.json points at the freshly-written revision.
	latest, _, err := statestore.ReadLatestRevisionRef(context.Background(), store)
	if err != nil {
		t.Fatalf("ReadLatestRevisionRef: %v", err)
	}
	if latest.RevisionKey != rev.RevisionKey {
		t.Errorf("latest.RevisionKey=%q want %q", latest.RevisionKey, rev.RevisionKey)
	}
}

func TestWriteRevision_CollisionAppendsX1(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	cfg := newWriterCfg(store, now)

	plan := []byte(`{"jobs":[]}`)
	hash := "feedface00112233"

	first, err := WriteRevision(context.Background(), cfg, trig, plan, hash)
	if err != nil {
		t.Fatalf("first WriteRevision: %v", err)
	}

	// Second invocation with identical (trigger, planHash) must collide and
	// resolve to -x1, NOT clobber the first revision body.
	second, err := WriteRevision(context.Background(), cfg, trig, plan, hash)
	if err != nil {
		t.Fatalf("second WriteRevision: %v", err)
	}
	if first.RevisionKey == second.RevisionKey {
		t.Fatalf("expected distinct keys, got %q twice", first.RevisionKey)
	}
	if !strings.HasSuffix(second.RevisionKey, "-x1") {
		t.Errorf("second key %q missing -x1 suffix", second.RevisionKey)
	}
	// Both revision bodies remain readable.
	for _, k := range []string{first.RevisionKey, second.RevisionKey} {
		if _, _, err := store.Read(context.Background(), statestore.RevisionDocPath(k)); err != nil {
			t.Errorf("revision body for %q missing: %v", k, err)
		}
	}
}

func TestWriteRevision_BootstrapAndRefUpdate(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))

	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Second write with a different planHash exercises the CAS path
	// (refs/latest-revision.json now exists, so we update via CAS rather
	// than CreateIfAbsent bootstrap).
	rev2, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "feedfacebadc0ffe")
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	latest, _, _ := statestore.ReadLatestRevisionRef(context.Background(), store)
	if latest.RevisionKey != rev2.RevisionKey {
		t.Errorf("latest=%q want %q", latest.RevisionKey, rev2.RevisionKey)
	}
}

func TestWriteRevision_RejectsInvalidInputs(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	trig := newTestTrigger(t)
	planHash := "deadbeefcafebabe"

	t.Run("nil store", func(t *testing.T) {
		bad := cfg
		bad.Store = nil
		if _, err := WriteRevision(context.Background(), bad, trig, []byte(`{}`), planHash); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("err=%v want ErrInvalid", err)
		}
	})
	t.Run("missing trigger fields", func(t *testing.T) {
		var empty triggerctx.TriggerOccurrence
		if _, err := WriteRevision(context.Background(), cfg, empty, []byte(`{}`), planHash); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("err=%v want ErrInvalid", err)
		}
	})
	t.Run("empty planBytes", func(t *testing.T) {
		if _, err := WriteRevision(context.Background(), cfg, trig, nil, planHash); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("err=%v want ErrInvalid", err)
		}
	})
	t.Run("bad planHash", func(t *testing.T) {
		if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "abc"); !errors.Is(err, statestore.ErrInvalid) {
			t.Errorf("err=%v want ErrInvalid", err)
		}
	})
}

func TestWriteRevision_CompatibilityToggle(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Now().UTC()).WithCompatibilityWrites(false)

	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe"); err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	// PR-A is a no-op stub for the compat branch in either direction; we
	// just ensure the flag plumbs through without surfacing an error. The
	// real assertion lands in M5 once the legacy mirror body is filled in.
}

func TestWriteRevision_ScopedTriggerRefWritten(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))

	rev, err := WriteRevision(context.Background(), cfg, trig, []byte(`{}`), "deadbeefcafebabe")
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	// Both refs/triggers/<name>/latest.json AND
	// refs/triggers/<name>/<scope>.json should exist.
	scoped, _, err := statestore.ReadTriggerRef(context.Background(), store, statestore.TriggerRefScope{
		Name: trig.TriggerName, Scope: trig.Source.SourceScope,
	})
	if err != nil {
		t.Fatalf("read scoped trigger ref: %v", err)
	}
	if scoped.RevisionKey != rev.RevisionKey {
		t.Errorf("scoped ref points at %q want %q", scoped.RevisionKey, rev.RevisionKey)
	}
}

func TestEnsureStateStoreVersion_Idempotent(t *testing.T) {
	store := newTestStore(t)
	now := func() time.Time { return time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC) }

	if err := EnsureStateStoreVersion(context.Background(), store, now); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := readVersionDoc(t, store)

	// Second call must be a no-op — the existing version.json is preserved.
	if err := EnsureStateStoreVersion(context.Background(), store, func() time.Time {
		return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	second := readVersionDoc(t, store)
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Errorf("version.json was overwritten: %v vs %v", first.CreatedAt, second.CreatedAt)
	}
	if second.Layout != StateStoreLayoutRevisionFirst || second.Version != StateStoreVersionCurrent {
		t.Errorf("version doc mismatch: %+v", second)
	}
}

func TestEnsureStateStoreVersion_NilGuards(t *testing.T) {
	if err := EnsureStateStoreVersion(context.Background(), nil, nil); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("nil store err=%v want ErrInvalid", err)
	}

	store := newTestStore(t)
	// nil clock is allowed — defaults to time.Now.
	if err := EnsureStateStoreVersion(context.Background(), store, nil); err != nil {
		t.Errorf("nil clock err=%v want nil", err)
	}
}
