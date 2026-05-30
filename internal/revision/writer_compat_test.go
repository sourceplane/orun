package revision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
)

func TestWriteRevision_CompatMirror_Default(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	// The legacy alias path uses the bare-hex full digest (no sha256:
	// prefix). Read it back through the same store.
	checksumPath, _ := legacyPlanPath(planHash)
	gotChecksum, _, err := store.Read(context.Background(), checksumPath)
	if err != nil {
		t.Fatalf("read legacy plan alias: %v", err)
	}
	if string(gotChecksum) != string(plan) {
		t.Errorf("legacy alias bytes diverged from canonical plan\n got=%s\nwant=%s", gotChecksum, plan)
	}
	latestPath, _ := legacyLatestPlanPath()
	gotLatest, _, err := store.Read(context.Background(), latestPath)
	if err != nil {
		t.Fatalf("read legacy latest: %v", err)
	}
	if string(gotLatest) != string(plan) {
		t.Errorf("legacy latest bytes diverged from canonical plan")
	}
	// Canonical plan still wins (no mutation).
	canonicalRaw, _, err := store.Read(context.Background(), statestore.PlanPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read canonical plan: %v", err)
	}
	if string(canonicalRaw) != string(plan) {
		t.Error("canonical plan mutated by compat mirror")
	}
}

func TestWriteRevision_CompatMirror_Disabled(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)).WithCompatibilityWrites(false)
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	if _, err := WriteRevision(context.Background(), cfg, trig, plan, planHash); err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	checksumPath, _ := legacyPlanPath(planHash)
	if _, _, err := store.Read(context.Background(), checksumPath); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("legacy alias unexpectedly written when CompatibilityWrites=false: err=%v", err)
	}
	latestPath, _ := legacyLatestPlanPath()
	if _, _, err := store.Read(context.Background(), latestPath); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("legacy latest unexpectedly written when CompatibilityWrites=false: err=%v", err)
	}
}

func TestWriteRevision_CompatMirror_StripsSha256Prefix(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan"}`)
	planHash := "sha256:feedface00112233445566778899aabbccddeeff00112233"
	if _, err := WriteRevision(context.Background(), cfg, trig, plan, planHash); err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	bareChecksum := "feedface00112233445566778899aabbccddeeff00112233"
	checksumPath, _ := legacyPlanPath(bareChecksum)
	if _, _, err := store.Read(context.Background(), checksumPath); err != nil {
		t.Errorf("legacy alias not at bare-hex path %q: %v", checksumPath, err)
	}
}

func TestWriteRevision_JobCountThreaded(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	cfg.JobCount = 42
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan"}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if rev.Summary.JobCount != 42 {
		t.Errorf("rev.Summary.JobCount = %d want 42", rev.Summary.JobCount)
	}
}

func TestNormalizeLegacyChecksum(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"feedface", "feedface", false},
		{"sha256:FEEDFACE00112233", "feedface00112233", false},
		{"  AaBb1234  ", "aabb1234", false},
		{"abc", "", true}, // < 8 chars
		{"sha256:gggggggg", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := normalizeLegacyChecksum(c.in)
		if c.err {
			if err == nil {
				t.Errorf("normalizeLegacyChecksum(%q) ok=%q want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeLegacyChecksum(%q) err=%v", c.in, err)
		}
		if got != c.want {
			t.Errorf("normalizeLegacyChecksum(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestLegacyPlanPath_Validation(t *testing.T) {
	if _, err := legacyPlanPath("bad/checksum"); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("legacyPlanPath bad: err=%v want ErrInvalid", err)
	}
	got, err := legacyPlanPath("feedface")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plans/feedface.json" {
		t.Errorf("legacyPlanPath = %q", got)
	}
	latest, err := legacyLatestPlanPath()
	if err != nil {
		t.Fatal(err)
	}
	if latest != "plans/latest.json" {
		t.Errorf("legacyLatestPlanPath = %q", latest)
	}
}

func TestIsHexLower(t *testing.T) {
	for _, in := range []string{"abcdef0123", "feedface", "00"} {
		if !isHexLower(in) {
			t.Errorf("isHexLower(%q) = false want true", in)
		}
	}
	for _, in := range []string{"", "ABCDEF", "abcg", "rev-foo", "12-34"} {
		if isHexLower(in) {
			t.Errorf("isHexLower(%q) = true want false", in)
		}
	}
}
