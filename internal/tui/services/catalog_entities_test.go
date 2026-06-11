package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadCatalog_ProjectsEntitiesAndRelations builds a real object-model
// catalog (the parity-test harness) and asserts LoadCatalog projects it into
// the cockpit's multi-kind snapshot: components alongside derived entities,
// counts per kind, and the typed relation graph.
func TestLoadCatalog_ProjectsEntitiesAndRelations(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	om := t.TempDir()

	write := func(rel, body string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("intent.yaml", cgParityIntent)
	write("libs/shared/main.go", "package shared\n")
	write("apps/api/main.go", "package api\n")
	write("apps/web/frontend/main.go", "package web\n")

	assembleFreshTestCatalog(t, ctx, ws, om)

	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: om})
	snap, err := s.LoadCatalog(ctx)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if snap == nil {
		t.Fatal("LoadCatalog returned nil snapshot for a populated store")
	}

	byKind := map[string][]EntitySummary{}
	for _, e := range snap.Entities {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	if got := len(byKind["Component"]); got != 3 {
		t.Fatalf("Component entities = %d, want 3 (kinds: %v)", got, kindsOf(byKind))
	}
	// The cgParity workspace declares domains platform + edge, so the resolver
	// derives Domain entities (SC3).
	if got := len(byKind["Domain"]); got < 2 {
		t.Fatalf("Domain entities = %d, want >= 2 (kinds: %v)", got, kindsOf(byKind))
	}

	// Counts must agree with the projected rows for every kind present.
	for kind, list := range byKind {
		if snap.CountsByKind[kind] != len(list) {
			t.Errorf("CountsByKind[%s] = %d, rows = %d", kind, snap.CountsByKind[kind], len(list))
		}
	}

	// Canonical ordering: Components sort before derived kinds.
	if snap.Entities[0].Kind != "Component" {
		t.Errorf("first entity kind = %s, want Component", snap.Entities[0].Kind)
	}

	// The api→shared and web→api dependsOn edges must appear in the typed
	// relation graph.
	var foundDep bool
	for _, r := range snap.Relations {
		if r.Type == "dependsOn" && r.FromKind == "Component" {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Errorf("relations missing Component dependsOn edges: %+v", snap.Relations)
	}
}

// TestLoadCatalog_AbsentStoreIsEmptyState ensures the best-effort contract: no
// object model → (nil, nil), never an error.
func TestLoadCatalog_AbsentStoreIsEmptyState(t *testing.T) {
	ctx := context.Background()

	s := NewLiveOrunService(LiveServiceConfig{})
	snap, err := s.LoadCatalog(ctx)
	if err != nil || snap != nil {
		t.Fatalf("no ObjectModelRoot: got (%v, %v), want (nil, nil)", snap, err)
	}

	s = NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: t.TempDir()})
	snap, err = s.LoadCatalog(ctx)
	if err != nil || snap != nil {
		t.Fatalf("empty object model: got (%v, %v), want (nil, nil)", snap, err)
	}
}

func kindsOf(byKind map[string][]EntitySummary) map[string]int {
	out := make(map[string]int, len(byKind))
	for k, v := range byKind {
		out[k] = len(v)
	}
	return out
}
