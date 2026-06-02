package nodewriter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

var errBoom = errors.New("boom")

func newWriter(t *testing.T) (*Writer, *objectstore.LocalStore, *refstore.LocalRefStore) {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{T: time.Unix(0, 0).UTC()}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	var n int
	idgen := func() string { n++; return fmt.Sprintf("trg_%03d", n) }
	w := New(store, refs, WithClock(clock.Fixed{T: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)}), WithIDGen(idgen))
	return w, store, refs
}

func samplePlan(withCatalog bool) PlanInput {
	in := PlanInput{
		Source:       nodes.SourceSnapshot{Scope: nodes.ScopeMain, Repo: "ns/repo", HeadRevision: "abc123"},
		SourceRefs:   []string{"refs/sources/current", "refs/sources/main"},
		Revision:     nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: 1},
		PlanBytes:    []byte(`{"plan":"A"}`),
		RevisionRefs: []string{"refs/revisions/latest"},
		Trigger: nodes.TriggerOccurrence{
			TriggerName: "system.manual", TriggerKey: "system.manual:full",
			Source: nodes.TriggerSource{Flavor: "system", System: "manual"},
			Scope:  nodes.RevisionScope{Mode: "full"}, Actor: "cli",
		},
		TriggerRefs: []string{"refs/triggers/system.manual/latest"},
	}
	if withCatalog {
		in.Catalog = &CatalogInput{
			Snapshot:  nodes.CatalogSnapshot{ResolverVersion: 1},
			Manifests: []nodes.ComponentManifest{{Identity: nodes.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api"}}},
			Refs:      []string{"refs/catalogs/main"},
		}
	}
	return in
}

func TestPlanHappyPathMovesRefs(t *testing.T) {
	t.Parallel()
	w, _, refs := newWriter(t)
	ctx := context.Background()
	res, err := w.Plan(ctx, samplePlan(true))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.SourceID == "" || res.CatalogID == "" || res.RevisionID == "" || res.TriggerID == "" {
		t.Fatalf("incomplete result: %+v", res)
	}
	if res.RevisionReused {
		t.Fatalf("first plan should not be reused")
	}
	checks := map[string]objectstore.ObjectID{
		"refs/sources/current":               res.SourceID,
		"refs/sources/main":                  res.SourceID,
		"refs/catalogs/main":                 res.CatalogID,
		"refs/revisions/latest":              res.RevisionID,
		"refs/triggers/system.manual/latest": res.TriggerID,
	}
	for name, want := range checks {
		ref, err := refs.Read(ctx, name)
		if err != nil {
			t.Fatalf("Read %s: %v", name, err)
		}
		if ref.Target != string(want) {
			t.Fatalf("ref %s = %s, want %s", name, ref.Target, want)
		}
	}
}

func TestPlanDedupAcrossTriggers(t *testing.T) {
	t.Parallel()
	w, _, _ := newWriter(t)
	ctx := context.Background()
	first, err := w.Plan(ctx, samplePlan(true))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := w.Plan(ctx, samplePlan(true))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.RevisionID != second.RevisionID {
		t.Fatalf("identical plan produced different revisions: %s vs %s", first.RevisionID, second.RevisionID)
	}
	if first.SourceID != second.SourceID || first.CatalogID != second.CatalogID {
		t.Fatalf("source/catalog not reused")
	}
	if second.RevisionReused != true {
		t.Fatalf("second plan should report revision reused")
	}
	if first.TriggerID == second.TriggerID {
		t.Fatalf("triggers should be distinct events: %s", first.TriggerID)
	}
}

func TestPlanDegenerateNoCatalog(t *testing.T) {
	t.Parallel()
	w, store, _ := newWriter(t)
	ctx := context.Background()
	res, err := w.Plan(ctx, samplePlan(false))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.CatalogID != "" {
		t.Fatalf("degenerate plan should have no catalog id, got %s", res.CatalogID)
	}
	// revision.json must carry no catalogId edge.
	entries, _ := store.GetTree(ctx, res.RevisionID)
	var revBlob objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "revision.json" {
			revBlob = e.ID
		}
	}
	_, body, _ := store.Get(ctx, revBlob)
	if want := "catalogId"; contains(string(body), want) {
		t.Fatalf("degenerate revision.json contains %q: %s", want, body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRecordTriggerMintsIDAndTime(t *testing.T) {
	t.Parallel()
	w, store, _ := newWriter(t)
	ctx := context.Background()
	rev := objectstore.ObjectID("sha256:" + repeat("a", 64))
	id, err := w.RecordTrigger(ctx, nodes.TriggerOccurrence{TriggerName: "n"}, rev)
	if err != nil {
		t.Fatalf("RecordTrigger: %v", err)
	}
	_, body, _ := store.Get(ctx, id)
	if !contains(string(body), `"triggerId":"trg_001"`) {
		t.Fatalf("minted id missing: %s", body)
	}
	if !contains(string(body), `"createdAt":"2026-06-02T10:00:00Z"`) {
		t.Fatalf("clock time missing: %s", body)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
