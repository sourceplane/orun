package revision

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// writeManifestPrep runs WriteRevision then WriteManifest with deterministic
// inputs and returns (cfg, rev, trig) so the per-test assertion can focus on
// what changed rather than re-deriving fixtures.
func writeManifestPrep(t *testing.T) (Config, PlanRevision, triggerctx.TriggerOccurrence) {
	t.Helper()
	store := newTestStore(t)
	trig := newTestTrigger(t)
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	cfg := newWriterCfg(store, now)
	cfg.JobCount = 12
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if err := WriteManifest(context.Background(), cfg, rev, trig); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	return cfg, rev, trig
}

func TestWriteManifest_Golden(t *testing.T) {
	cfg, rev, trig := writeManifestPrep(t)

	raw, _, err := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}

	var m RevisionManifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}

	if m.APIVersion != APIVersion || m.Kind != ManifestKind {
		t.Errorf("apiVersion/kind = %q/%q", m.APIVersion, m.Kind)
	}
	if m.Revision.Key != rev.RevisionKey {
		t.Errorf("revision.key = %q want %q", m.Revision.Key, rev.RevisionKey)
	}
	if m.Revision.PlanHash != rev.PlanHash {
		t.Errorf("revision.planHash = %q want %q", m.Revision.PlanHash, rev.PlanHash)
	}
	if m.Trigger.Key != trig.TriggerKey {
		t.Errorf("trigger.key = %q want %q", m.Trigger.Key, trig.TriggerKey)
	}
	if m.Trigger.Type != trig.TriggerType || m.Trigger.Name != trig.TriggerName {
		t.Errorf("trigger.type/name = %q/%q", m.Trigger.Type, m.Trigger.Name)
	}
	if m.Trigger.Scope != trig.PlanScope.Mode {
		t.Errorf("trigger.scope = %q want %q", m.Trigger.Scope, trig.PlanScope.Mode)
	}
	if m.Source != trig.Source {
		t.Errorf("source = %+v want %+v", m.Source, trig.Source)
	}
	if m.Summary.JobCount != 12 {
		t.Errorf("summary.jobCount = %d want 12", m.Summary.JobCount)
	}
	if len(m.Summary.ActiveEnvironments) != 1 || m.Summary.ActiveEnvironments[0] != "prod" {
		t.Errorf("summary.activeEnvironments = %v", m.Summary.ActiveEnvironments)
	}
	if m.Summary.LatestExecutionKey != "" {
		t.Errorf("summary.latestExecutionKey unexpectedly set: %q", m.Summary.LatestExecutionKey)
	}
	if m.Objects.Plan != "plan.json" || m.Objects.Trigger != "trigger.json" || m.Objects.Revision != "revision.json" {
		t.Errorf("objects = %+v", m.Objects)
	}
}

func TestWriteManifest_Idempotent(t *testing.T) {
	cfg, rev, trig := writeManifestPrep(t)
	first, _, err := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if err := WriteManifest(context.Background(), cfg, rev, trig); err != nil {
		t.Fatalf("WriteManifest #2: %v", err)
	}
	second, _, err := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("manifest bytes diverged on second write\n#1=%s\n#2=%s", first, second)
	}
}

func TestWriteManifest_NilStore(t *testing.T) {
	err := WriteManifest(context.Background(), Config{}, PlanRevision{RevisionKey: "rev-main-abcdef0-pfeedface"}, triggerctx.TriggerOccurrence{})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

func TestWriteManifest_RejectsBadKey(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	rev := PlanRevision{RevisionKey: "not a key"}
	err := WriteManifest(context.Background(), cfg, rev, newTestTrigger(t))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

func TestWriteManifest_RejectsBadTrigger(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	bad := newTestTrigger(t)
	bad.TriggerKey = ""
	err := WriteManifest(context.Background(), cfg, PlanRevision{RevisionKey: "rev-main-abcdef0-pfeedface"}, bad)
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}

func TestUpdateLatestExecutionSummary_HappyPath(t *testing.T) {
	cfg, rev, _ := writeManifestPrep(t)
	exec := LatestExecutionSummary{Key: "run-001", Status: "completed"}
	if err := UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey, exec); err != nil {
		t.Fatalf("UpdateLatestExecutionSummary: %v", err)
	}
	raw, _, err := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m RevisionManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Summary.LatestExecutionKey != "run-001" {
		t.Errorf("latestExecutionKey = %q", m.Summary.LatestExecutionKey)
	}
	if m.Summary.LatestExecutionStatus != "completed" {
		t.Errorf("latestExecutionStatus = %q", m.Summary.LatestExecutionStatus)
	}
}

func TestUpdateLatestExecutionSummary_Idempotent(t *testing.T) {
	cfg, rev, _ := writeManifestPrep(t)
	exec := LatestExecutionSummary{Key: "run-001", Status: "running"}
	for i := 0; i < 3; i++ {
		if err := UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey, exec); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	raw, _, _ := cfg.Store.Read(context.Background(), statestore.ManifestPath(rev.RevisionKey))
	var m RevisionManifest
	_ = json.Unmarshal(raw, &m)
	if m.Summary.LatestExecutionKey != "run-001" || m.Summary.LatestExecutionStatus != "running" {
		t.Errorf("post-3x summary = %+v", m.Summary)
	}
}

func TestUpdateLatestExecutionSummary_NotFound(t *testing.T) {
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	err := UpdateLatestExecutionSummary(context.Background(), cfg, "rev-main-abcdef0-pfeedface", LatestExecutionSummary{Key: "x", Status: "y"})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound", err)
	}
}

func TestUpdateLatestExecutionSummary_NilStoreAndBadKey(t *testing.T) {
	if err := UpdateLatestExecutionSummary(context.Background(), Config{}, "rev-main-abcdef0-pfeedface", LatestExecutionSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("nil store err=%v", err)
	}
	store := newTestStore(t)
	cfg := newWriterCfg(store, time.Now().UTC())
	if err := UpdateLatestExecutionSummary(context.Background(), cfg, "bad key", LatestExecutionSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("bad key err=%v", err)
	}
}

func TestUpdateLatestExecutionSummary_DecodeError(t *testing.T) {
	cfg, rev, _ := writeManifestPrep(t)
	// Corrupt manifest.
	if _, err := cfg.Store.Write(context.Background(), statestore.ManifestPath(rev.RevisionKey), []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	err := UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey, LatestExecutionSummary{Key: "x"})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("err=%v want ErrInvalid", err)
	}
}
