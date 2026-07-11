package worklens

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func TestParseSpecTreeGolden(t *testing.T) {
	plan, err := ParseSpecTree(filepath.Join("testdata", "spectree"), "ws_test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "import-plan.golden.json")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("import plan drifted from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseSpecTreeShape(t *testing.T) {
	plan, err := ParseSpecTree(filepath.Join("testdata", "spectree"), "ws_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Specs) != 2 {
		t.Fatalf("specs = %d, want 2 (archive skipped)", len(plan.Specs))
	}
	if plan.Specs[0].Slug != "demo-epic" || plan.Specs[1].Slug != "docs-only" {
		t.Fatalf("spec order = %s, %s", plan.Specs[0].Slug, plan.Specs[1].Slug)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}

	d0 := plan.Tasks[0]
	if d0.MilestoneID != "D0" || d0.Title != "Lay the substrate" {
		t.Errorf("D0 parsed as %q %q", d0.MilestoneID, d0.Title)
	}
	if d0.Contract == nil || d0.Contract.Goal != "the two tables exist and replay" {
		t.Errorf("D0 goal = %+v", d0.Contract)
	}
	if len(d0.Contract.DoneWhen) != 2 {
		t.Errorf("D0 doneWhen = %v", d0.Contract.DoneWhen)
	}
	if len(d0.Contract.Deps) != 0 {
		t.Errorf("D0 deps = %v (prose should carry no tokens)", d0.Contract.Deps)
	}
	if d0.Contract.GatesDefined || len(d0.Contract.Gates) > 0 {
		t.Error("import must not declare gates (P-7 honest degradation)")
	}

	d1 := plan.Tasks[1]
	if len(d1.Contract.Deps) != 1 || d1.Contract.Deps[0] != "D0" {
		t.Errorf("D1 deps = %v", d1.Contract.Deps)
	}

	// Lifecycle is never imported: contracts undeclare gates, so even a
	// merged history parks In Review rather than lying Done — and the plan
	// itself has no status field of any kind.
	raw, _ := json.Marshal(plan)
	for _, forbidden := range []string{`"status"`, `"rung"`, `"lifecycle"`} {
		if containsBytes(raw, forbidden) {
			t.Errorf("import plan carries %s — lifecycle must derive, never import", forbidden)
		}
	}
}

// TestParseRealSpecTree smoke-parses this repo's own specs/ tree — the
// dogfood corpus (WP0 "orun plans orun").
func TestParseRealSpecTree(t *testing.T) {
	root := filepath.Join("..", "..", "specs")
	if _, err := os.Stat(root); err != nil {
		t.Skip("specs tree not present")
	}
	plan, err := ParseSpecTree(root, "ws_dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Specs) < 5 {
		t.Fatalf("only %d specs parsed from the real tree", len(plan.Specs))
	}
	var orunWork *ImportSpec
	for i := range plan.Specs {
		if plan.Specs[i].Slug == "orun-work" {
			orunWork = &plan.Specs[i]
		}
	}
	if orunWork == nil {
		t.Fatal("orun-work epic not parsed from the real tree")
	}
	milestones := 0
	for _, task := range plan.Tasks {
		if task.SpecSlug == "orun-work" {
			milestones++
		}
	}
	if milestones != 6 {
		t.Errorf("orun-work milestones = %d, want 6 (WP0–WP5)", milestones)
	}
}

func containsBytes(b []byte, s string) bool {
	return len(s) > 0 && len(b) >= len(s) && (string(b) == s || indexOf(string(b), s) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestFoldBudgetOnDogfoodCorpus measures the delivery fold + the intent and
// rollup folds over this repo's real specs tree — the WH6 budget record
// (P-1 heritage: lifecycle-at-read must stay cheap on a hot workspace).
// The number lands in specs/epics/orun-work-v4/implementation-plan.md.
func TestFoldBudgetOnDogfoodCorpus(t *testing.T) {
	root := filepath.Join("..", "..", "specs")
	if _, err := os.Stat(root); err != nil {
		t.Skip("specs tree not present")
	}
	plan, err := ParseSpecTree(root, "ws_dev")
	if err != nil {
		t.Fatal(err)
	}
	var tasks []Task
	var events []CoordinationEvent
	seq := int64(0)
	for i, it := range plan.Tasks {
		key := fmt.Sprintf("WRK-%d", i+1)
		tasks = append(tasks, Task{
			APIVersion: APIVersion, Kind: KindTask, Key: key, Workspace: "ws_dev",
			Spec: it.SpecSlug, Milestone: it.Milestone, Title: it.Title, Contract: it.Contract,
			CreatedBy: Actor{Type: ActorUser, ID: "usr_1", Via: "import"},
		})
	}
	for _, m := range plan.Milestones {
		seq++
		payload, _ := json.Marshal(map[string]interface{}{"op": "create", "key": m.Key, "title": m.Title, "ordinal": m.Ordinal})
		events = append(events, CoordinationEvent{
			Workspace: "ws_dev", Subject: m.SpecSlug, Kind: EventMilestoneEdited,
			Actor: Actor{Type: ActorUser, ID: "usr_1", Via: "import"},
			At:    "2026-07-11T10:00:00Z", Payload: payload, Seq: seq,
		})
	}
	ws := WorkSet{Tasks: tasks, Events: events}

	const rounds = 50
	start := time.Now()
	for i := 0; i < rounds; i++ {
		fr := Fold(ws)
		for _, spec := range plan.Specs {
			ladder := FoldMilestones(spec.Slug, events)
			_ = FoldEpicExecution(ws, spec.Slug, ladder, fr)
			_ = FoldEpicIntent(spec.Slug, events)
		}
	}
	per := time.Since(start) / rounds
	t.Logf("dogfood corpus: %d specs, %d milestones, %d tasks — full workspace fold+rollups: %s/round", len(plan.Specs), len(plan.Milestones), len(plan.Tasks), per)
	if per > 250*time.Millisecond {
		t.Fatalf("fold budget blown: %s/round (budget 250ms)", per)
	}
}
