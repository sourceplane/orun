package revision

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// TestResolveRevision_CorruptRevisionDoc trips the strict-decode branch in
// resolveFromRevisionKey by overwriting revision.json with garbage after a
// good WriteRevision.
func TestResolveRevision_CorruptRevisionDoc(t *testing.T) {
	store, rev, _ := writeFixture(t)
	if _, err := store.Write(context.Background(),
		statestore.RevisionDocPath(rev.RevisionKey),
		[]byte("not-json"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveRevision(context.Background(), store, rev.RevisionKey, ResolveOptions{})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

func TestResolveRevision_CorruptTriggerDoc(t *testing.T) {
	store, rev, _ := writeFixture(t)
	if _, err := store.Write(context.Background(),
		statestore.TriggerPath(rev.RevisionKey),
		[]byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveRevision(context.Background(), store, rev.RevisionKey, ResolveOptions{})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

// TestResolveRevision_NamedRefDanglingTarget covers the
// "named ref → revision lookup fails" arm.
func TestResolveRevision_NamedRefDanglingTarget(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := statestore.WriteNamedRef(context.Background(), store, "ghost", statestore.NamedRef{
		Name:        "ghost",
		RevisionKey: "rev-main-abcdef0-pdeadbeef",
		RevisionID:  "rev_x",
		PlanHash:    "sha256:deadbeef00000000000000000000000000000000000000000000000000000000",
		CreatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveRevision(context.Background(), store, "ghost", ResolveOptions{})
	if err == nil {
		t.Fatal("want err")
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound chain", err)
	}
}

func TestResolveRevision_LegacyHashShortRejected(t *testing.T) {
	// Hex but < 8 chars: branch 5 normalize fails → falls to branch 7.
	store := newTestStore(t)
	_, err := ResolveRevision(context.Background(), store, "abc", ResolveOptions{})
	if !errors.Is(err, ErrAmbiguousArg) {
		t.Errorf("err=%v want ErrAmbiguousArg", err)
	}
}

// TestSynthesizeRevision_BadHash covers the PlanShortHash error arm.
func TestSynthesizeRevision_BadHash(t *testing.T) {
	trig := triggerctx.NewSystemManual(triggerctx.SystemOptions{
		Source: triggerctx.TriggerSource{
			Repo: "git@example.com:o/r.git", Ref: "refs/heads/main",
			SourceScope: "main", HeadRevision: "abcdef0",
			WorkingTree: triggerctx.WorkingTreeClean,
		},
		PlanScope: triggerctx.PlanScope{Mode: triggerctx.PlanScopeFull},
		Now:       time.Now().UTC(),
	})
	if _, err := synthesizeRevision(trig, "abc", time.Now().UTC()); err == nil {
		t.Error("synthesizeRevision with short hash returned no error")
	}
}

// TestIsExistingFile_BareStringSkipsStat verifies the early-return branch
// that avoids a syscall for arg shapes that obviously aren't paths.
func TestIsExistingFile_BareStringSkipsStat(t *testing.T) {
	if isExistingFile("") {
		t.Error("empty string is not a file")
	}
	if isExistingFile("just-a-name") {
		t.Error("name without separators is not a file")
	}
}

// TestWriteCompatibilityMirror_BadPlanHash exercises the
// normalizeLegacyChecksum failure arm in writeCompatibilityMirror.
func TestWriteCompatibilityMirror_BadPlanHash(t *testing.T) {
	store := newTestStore(t)
	rev := PlanRevision{PlanHash: "abc"} // < 8 hex
	err := writeCompatibilityMirror(context.Background(), store, rev, []byte("{}"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

// TestUpdateLatestExecutionSummary_AppliesAcrossExecs verifies the second
// CAS branch (different exec key replaces previous).
func TestUpdateLatestExecutionSummary_AppliesAcrossExecs(t *testing.T) {
	cfg, rev, _ := writeManifestPrep(t)
	if err := UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey,
		LatestExecutionSummary{Key: "run-1", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey,
		LatestExecutionSummary{Key: "run-2", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	raw, _, _ := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	var m RevisionManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Summary.LatestExecutionKey != "run-2" {
		t.Errorf("latestExecutionKey=%q want run-2", m.Summary.LatestExecutionKey)
	}
	if m.Summary.LatestExecutionStatus != "completed" {
		t.Errorf("latestExecutionStatus=%q want completed", m.Summary.LatestExecutionStatus)
	}
}

// TestManifestFrom_NilActiveEnvs documents that a nil ActiveEnvironments
// slice is rendered as [] (the canonical encoder forbids null slices in
// our payload conventions).
func TestManifestFrom_NilActiveEnvs(t *testing.T) {
	rev := PlanRevision{
		RevisionKey: "rev-main-abcdef0-pfeedface",
		Summary:     RevSummary{},
		CreatedAt:   time.Now().UTC(),
	}
	trig := newTestTrigger(t)
	m := manifestFrom(rev, trig)
	if m.Summary.ActiveEnvironments == nil {
		t.Error("ActiveEnvironments nil; want empty slice")
	}
	if len(m.Summary.ActiveEnvironments) != 0 {
		t.Errorf("ActiveEnvironments=%v want empty", m.Summary.ActiveEnvironments)
	}
}
