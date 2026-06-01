package revision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// writeFixture lays down a complete revision via WriteRevision and returns
// the store + the ref data resolver tests assert against.
func writeFixture(t *testing.T) (statestore.StateStore, PlanRevision, []byte) {
	t.Helper()
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
	return store, rev, plan
}

func TestResolveRevision_Branch1_EmptyArg(t *testing.T) {
	store, want, _ := writeFixture(t)
	got, err := ResolveRevision(context.Background(), store, "", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceLatest {
		t.Errorf("Source=%q want %q", got.Source, ResolveSourceLatest)
	}
	if got.Revision.RevisionKey != want.RevisionKey {
		t.Errorf("RevisionKey=%q want %q", got.Revision.RevisionKey, want.RevisionKey)
	}
	if got.Synthesized {
		t.Error("Synthesized should be false for branch 1")
	}
}

func TestResolveRevision_Branch1_NoLatestRef(t *testing.T) {
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound", err)
	}
}

func TestResolveRevision_Branch2_PlanFile(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	planBytes := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err := ResolveRevision(context.Background(), store, planPath, ResolveOptions{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceFile {
		t.Errorf("Source=%q want %q", got.Source, ResolveSourceFile)
	}
	if !got.Synthesized {
		t.Error("Synthesized=false; want true for branch 2")
	}
	if got.FilePath != planPath {
		t.Errorf("FilePath=%q want %q", got.FilePath, planPath)
	}
	if got.Trigger.TriggerName != triggerctx.SystemReplay {
		t.Errorf("TriggerName=%q want %q", got.Trigger.TriggerName, triggerctx.SystemReplay)
	}
	if string(got.PlanBytes) != string(planBytes) {
		t.Error("PlanBytes did not match file content")
	}
	// Synthesized revisions are NOT persisted.
	_, _, err = store.Read(context.Background(), statestore.RevisionDocPath(got.Revision.RevisionKey))
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("synthesized revision unexpectedly persisted: err=%v", err)
	}
}

func TestResolveRevision_Branch3_RevisionKey(t *testing.T) {
	store, want, plan := writeFixture(t)
	got, err := ResolveRevision(context.Background(), store, want.RevisionKey, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceRevisionKey {
		t.Errorf("Source=%q", got.Source)
	}
	if string(got.PlanBytes) != string(plan) {
		t.Error("plan bytes mismatch")
	}
}

func TestResolveRevision_RevisionPrefixViaGlobalIndex(t *testing.T) {
	store, want, plan := writeFixture(t)
	prefix := want.RevisionKey[:len(want.RevisionKey)-4]

	got, err := ResolveRevision(context.Background(), store, prefix, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision(%q): %v", prefix, err)
	}
	if got.Source != ResolveSourceRevisionKey {
		t.Errorf("Source=%q want %q", got.Source, ResolveSourceRevisionKey)
	}
	if got.Revision.RevisionKey != want.RevisionKey {
		t.Errorf("RevisionKey=%q want %q", got.Revision.RevisionKey, want.RevisionKey)
	}
	if string(got.PlanBytes) != string(plan) {
		t.Error("plan bytes mismatch")
	}
}

func TestResolveRevision_RevisionPrefixConflict(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{"jobs":[]}`), "feedface00112233445566778899aabbccddeeff00112233"); err != nil {
		t.Fatalf("first WriteRevision: %v", err)
	}
	if _, err := WriteRevision(context.Background(), cfg, trig, []byte(`{"jobs":[1]}`), "deadbeef00112233445566778899aabbccddeeff00112233"); err != nil {
		t.Fatalf("second WriteRevision: %v", err)
	}

	_, err := ResolveRevision(context.Background(), store, "rev-main-abcdef0-p", ResolveOptions{})
	if !errors.Is(err, statestore.ErrConflict) {
		t.Fatalf("err=%v want ErrConflict", err)
	}
}

func TestResolveFromRevisionPrefix_NotFoundAndListError(t *testing.T) {
	store := newTestStore(t)
	_, err := resolveFromRevisionPrefix(context.Background(), store, "")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("empty-prefix err=%v want ErrNotFound", err)
	}

	_, err = resolveFromRevisionPrefix(context.Background(), store, "rev-does-not-exist")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("not-found err=%v want ErrNotFound", err)
	}

	sentinel := errors.New("list denied")
	_, err = resolveFromRevisionPrefix(context.Background(), listErrStore{StateStore: store, err: sentinel}, "rev-")
	if !errors.Is(err, sentinel) {
		t.Fatalf("list err=%v want %v", err, sentinel)
	}
}

func TestResolveRevision_IndexReadErrorPropagates(t *testing.T) {
	store, want, _ := writeFixture(t)
	sentinel := errors.New("index read denied")
	wrapped := readPathErrStore{
		StateStore: store,
		path:       statestore.RevisionIndexPath(want.RevisionKey),
		err:        sentinel,
	}
	_, err := ResolveRevision(context.Background(), wrapped, want.RevisionKey, ResolveOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v want %v", err, sentinel)
	}
}

func TestResolveRevision_IndexPathSuccess(t *testing.T) {
	store, want, plan := writeFixture(t)
	catalogDir := "sources/src-branch-main-abcdef0/catalogs/cat-abcdef/revisions/" + want.RevisionKey
	for _, item := range []struct {
		from string
		name string
	}{
		{statestore.PlanPath(want.RevisionKey), "plan.json"},
		{statestore.RevisionDocPath(want.RevisionKey), "revision.json"},
		{statestore.TriggerPath(want.RevisionKey), "trigger.json"},
	} {
		raw, _, err := store.Read(context.Background(), item.from)
		if err != nil {
			t.Fatalf("read %s: %v", item.from, err)
		}
		if _, err := store.Write(context.Background(), catalogDir+"/"+item.name, raw, statestore.WriteOptions{}); err != nil {
			t.Fatalf("write catalog %s: %v", item.name, err)
		}
	}
	entry := statestore.RevisionIndexEntry{
		RevisionKey: want.RevisionKey,
		RevisionID:  want.RevisionID,
		TriggerKey:  want.TriggerKey,
		PlanHash:    want.PlanHash,
		CreatedAt:   want.CreatedAt,
		Path:        catalogDir,
	}
	if _, err := store.Write(context.Background(), statestore.RevisionIndexPath(want.RevisionKey), marshalCanonicalJSON(entry), statestore.WriteOptions{}); err != nil {
		t.Fatalf("overwrite revision index: %v", err)
	}

	got, err := ResolveRevision(context.Background(), store, want.RevisionKey, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Revision.RevisionKey != want.RevisionKey || string(got.PlanBytes) != string(plan) {
		t.Fatalf("catalog-path resolution mismatch: rev=%q plan=%q", got.Revision.RevisionKey, got.PlanBytes)
	}
}

func TestResolveRevision_IndexPathFallbackToGlobal(t *testing.T) {
	store, want, plan := writeFixture(t)
	badEntry := statestore.RevisionIndexEntry{
		RevisionKey: want.RevisionKey,
		RevisionID:  want.RevisionID,
		TriggerKey:  want.TriggerKey,
		PlanHash:    want.PlanHash,
		CreatedAt:   want.CreatedAt,
		Path:        "sources/src-branch-main-abcdef0/catalogs/cat-abcdef/revisions/" + want.RevisionKey,
	}
	if _, err := store.Write(context.Background(), statestore.RevisionIndexPath(want.RevisionKey), marshalCanonicalJSON(badEntry), statestore.WriteOptions{}); err != nil {
		t.Fatalf("overwrite revision index: %v", err)
	}

	got, err := ResolveRevision(context.Background(), store, want.RevisionKey, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Revision.RevisionKey != want.RevisionKey || string(got.PlanBytes) != string(plan) {
		t.Fatalf("fallback mismatch: rev=%q plan=%q", got.Revision.RevisionKey, got.PlanBytes)
	}
}

func TestResolveFromRevisionDir_ReadErrors(t *testing.T) {
	store, want, _ := writeFixture(t)
	if _, err := resolveFromRevisionDir(context.Background(), store, "", want.RevisionKey); !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("empty dir err=%v want ErrNotFound", err)
	}
	missingDir := "sources/src-branch-main-abcdef0/catalogs/cat-abcdef/revisions/" + want.RevisionKey
	if _, err := resolveFromRevisionDir(context.Background(), store, missingDir, want.RevisionKey); !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("missing plan err=%v want ErrNotFound", err)
	}
	planRaw, _, err := store.Read(context.Background(), statestore.PlanPath(want.RevisionKey))
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if _, err := store.Write(context.Background(), missingDir+"/plan.json", planRaw, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if _, err := resolveFromRevisionDir(context.Background(), store, missingDir, want.RevisionKey); !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("missing revision err=%v want ErrNotFound", err)
	}
	revRaw, _, err := store.Read(context.Background(), statestore.RevisionDocPath(want.RevisionKey))
	if err != nil {
		t.Fatalf("read revision: %v", err)
	}
	if _, err := store.Write(context.Background(), missingDir+"/revision.json", revRaw, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write revision: %v", err)
	}
	if _, err := resolveFromRevisionDir(context.Background(), store, missingDir, want.RevisionKey); !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("missing trigger err=%v want ErrNotFound", err)
	}
}

func TestResolveRevision_Branch4_NamedRef(t *testing.T) {
	store, want, _ := writeFixture(t)
	// Plant a named ref pointing at the persisted revision.
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := statestore.WriteNamedRef(context.Background(), store, "release", statestore.NamedRef{
		Name:        "release",
		RevisionKey: want.RevisionKey,
		RevisionID:  want.RevisionID,
		PlanHash:    want.PlanHash,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("WriteNamedRef: %v", err)
	}
	got, err := ResolveRevision(context.Background(), store, "release", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceNamedRef {
		t.Errorf("Source=%q want named-ref", got.Source)
	}
	if got.NamedRefName != "release" {
		t.Errorf("NamedRefName=%q", got.NamedRefName)
	}
	if got.Revision.RevisionKey != want.RevisionKey {
		t.Errorf("RevisionKey=%q want %q", got.Revision.RevisionKey, want.RevisionKey)
	}
}

func TestResolveRevision_Branch5_LegacyHash(t *testing.T) {
	store := newTestStore(t)
	planBytes := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","legacy":true}`)
	checksum := "deadbeefcafebabe1122334455667788"
	path, err := legacyPlanPath(checksum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write(context.Background(), path, planBytes, statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed legacy plan: %v", err)
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err := ResolveRevision(context.Background(), store, checksum, ResolveOptions{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceLegacyHash {
		t.Errorf("Source=%q want legacy-plan-hash", got.Source)
	}
	if !got.Synthesized {
		t.Error("Synthesized=false; want true for branch 5")
	}
	if got.LegacyPath != checksum {
		t.Errorf("LegacyPath=%q want %q", got.LegacyPath, checksum)
	}
	if got.Trigger.TriggerName != triggerctx.SystemMigrated {
		t.Errorf("TriggerName=%q want %q", got.Trigger.TriggerName, triggerctx.SystemMigrated)
	}
	if got.Trigger.Mode != triggerctx.ModeMigration {
		t.Errorf("Mode=%q want %q", got.Trigger.Mode, triggerctx.ModeMigration)
	}
}

func TestResolveRevision_Branch6_ComponentName(t *testing.T) {
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "my-component", ResolveOptions{
		IsComponentName: func(s string) bool { return s == "my-component" },
	})
	if !errors.Is(err, ErrComponentRunUnchanged) {
		t.Errorf("err=%v want ErrComponentRunUnchanged", err)
	}
}

func TestResolveRevision_Branch7_Ambiguous(t *testing.T) {
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "no-such-thing", ResolveOptions{})
	if !errors.Is(err, ErrAmbiguousArg) {
		t.Errorf("err=%v want ErrAmbiguousArg", err)
	}
}

func TestResolveRevision_Branch7_HexNoLegacy(t *testing.T) {
	// Hex-shaped arg without a legacy plan on disk → falls through to
	// branch 7 (no component matcher provided).
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "deadbeefcafebabe", ResolveOptions{})
	if !errors.Is(err, ErrAmbiguousArg) {
		t.Errorf("err=%v want ErrAmbiguousArg", err)
	}
}

func TestResolveRevision_NilStore(t *testing.T) {
	_, err := ResolveRevision(context.Background(), nil, "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

func TestResolveRevision_Branch3_NotFoundFallsThroughToBranch7(t *testing.T) {
	// Revision key regex matches but the revision was never written. The
	// resolver falls through to subsequent branches; with no named ref,
	// no legacy file, and no component matcher, branch 7 wins.
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "rev-main-abcdef0-pfeedface", ResolveOptions{})
	if !errors.Is(err, ErrAmbiguousArg) {
		t.Errorf("err=%v want ErrAmbiguousArg", err)
	}
}

func TestResolveRevision_FilePathPrecedesRevisionKey(t *testing.T) {
	// Even an arg that is also a revision-key shape — but happens to
	// exist on disk as a regular file — wins branch 2 per spec ordering.
	dir := t.TempDir()
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	planBytes := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	_, err := WriteRevision(context.Background(), cfg, trig, planBytes, planHash)
	if err != nil {
		t.Fatal(err)
	}

	// Make a real file whose name has '/' and '.' so isExistingFile
	// returns true. Using a path with separators ensures it doesn't get
	// confused with a revision key.
	overlay := filepath.Join(dir, "overlay-plan.json")
	overlayBytes := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","overlay":true}`)
	if err := os.WriteFile(overlay, overlayBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRevision(context.Background(), store, overlay, ResolveOptions{
		Now: func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Source != ResolveSourceFile {
		t.Errorf("Source=%q want %q", got.Source, ResolveSourceFile)
	}
	if string(got.PlanBytes) != string(overlayBytes) {
		t.Error("plan bytes did not match overlay")
	}
}

type listErrStore struct {
	statestore.StateStore
	err error
}

func (s listErrStore) List(context.Context, string) ([]statestore.ObjectInfo, error) {
	return nil, s.err
}

type readPathErrStore struct {
	statestore.StateStore
	path string
	err  error
}

func (s readPathErrStore) Read(ctx context.Context, p string) ([]byte, statestore.ObjectMeta, error) {
	if p == s.path {
		return nil, statestore.ObjectMeta{}, s.err
	}
	return s.StateStore.Read(ctx, p)
}
