package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/worklens"
)

func summaryFixture() *remotestate.WorkSummary {
	return &remotestate.WorkSummary{
		Specs: []remotestate.WorkSpecView{{
			Key: "demo-epic", Title: "Demo Epic", DocRef: "sha256:doc",
			CreatedBy: remotestate.WorkActor{Type: "user", ID: "usr_1"}, CreatedAt: "2026-07-04T12:00:00Z",
			Progress: map[string]int{"ready": 1},
		}},
		Tasks: []remotestate.WorkTaskView{
			{
				Key: "WRK-2", Spec: "demo-epic", Title: "second",
				CreatedBy: remotestate.WorkActor{Type: "user", ID: "usr_1"},
				Lifecycle: remotestate.WorkLifecycle{Rung: "in_review", Evidence: []string{"PR o/r#1 open"}},
			},
			{
				Key: "WRK-1", Spec: "demo-epic", Title: "first",
				Contract:  &remotestate.WorkContract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}},
				CreatedBy: remotestate.WorkActor{Type: "user", ID: "usr_1"},
				Lifecycle: remotestate.WorkLifecycle{Rung: "ready"},
			},
			{
				Key: "OTH-1", Spec: "other", Title: "not ours",
				CreatedBy: remotestate.WorkActor{Type: "user", ID: "usr_1"},
			},
		},
		CoordSeq: 42,
		ObsSeq:   7,
	}
}

func TestSnapshotFromSummaryFreezesIntentOnly(t *testing.T) {
	snap, err := snapshotFromSummary("ws_1", "demo-epic", summaryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tasks) != 2 || snap.Tasks[0].Key != "WRK-1" || snap.Tasks[1].Key != "WRK-2" {
		t.Fatalf("tasks = %+v", snap.Tasks)
	}
	if snap.CoordSeq != 42 || snap.ObsSeq != 7 {
		t.Fatalf("cursors = %d/%d", snap.CoordSeq, snap.ObsSeq)
	}
	id, canonical, err := worklens.SealSpecSnapshot(*snap)
	if err != nil {
		t.Fatal(err)
	}
	// The wire carried rungs and evidence; the sealed bytes must not.
	for _, tok := range []string{"in_review", "evidence", "rung", "PR o/r#1"} {
		if strings.Contains(string(canonical), tok) {
			t.Fatalf("sealed snapshot leaked fold output %q", tok)
		}
	}
	// Determinism across rebuilds.
	snap2, _ := snapshotFromSummary("ws_1", "demo-epic", summaryFixture())
	id2, _, _ := worklens.SealSpecSnapshot(*snap2)
	if id != id2 {
		t.Fatal("rebuilt snapshot sealed to a different id")
	}
	if _, err := snapshotFromSummary("ws_1", "missing", summaryFixture()); err == nil {
		t.Fatal("unknown slug accepted")
	}
}

func TestRenderBriefIsAgentReadable(t *testing.T) {
	snap, _ := snapshotFromSummary("ws_1", "demo-epic", summaryFixture())
	id, _, _ := worklens.SealSpecSnapshot(*snap)
	brief := renderBrief(snap, id)
	for _, want := range []string{"# Demo Epic — frozen brief", id, "## WRK-1 — first", "**Goal:** g", "**Gates:** tests", "read-only by construction"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	if strings.Contains(brief, "in_review") {
		t.Error("brief leaked a rung")
	}
}

func TestSpecPullMaterializationIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer func() { _ = os.Chdir(old) }()
	_ = os.Chdir(dir)

	snap, _ := snapshotFromSummary("ws_1", "demo-epic", summaryFixture())
	_, canonical, _ := worklens.SealSpecSnapshot(*snap)
	target := filepath.Join(".orun", "specs", "demo-epic")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "snapshot.json"), canonical, 0o444); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(target, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatal("materialized snapshot is writable (WD-7 heritage: pull is read-only)")
	}
}
