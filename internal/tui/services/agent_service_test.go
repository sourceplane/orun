package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadAgentTypes_ProjectsFromCatalog builds a real object-model catalog
// from a workspace carrying an agents/*.md and asserts LoadAgentTypes reads the
// projected AgentType entity — the persona resolved from its content blob and
// the mayAffect glob resolved to the component key.
func TestLoadAgentTypes_ProjectsFromCatalog(t *testing.T) {
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
	write("agents/implementer.md", `---
name: implementer
kind: agent-type
apiVersion: orun.io/v1
harness: claude-code
model: claude-opus-4-8
autonomyDefault: assist
tools:
  allow: [work_get]
  deny: ["*"]
mayAffect: [shared]
owner: team/platform
extends: base-orun-literacy
---
# Implementer

One Ready task to a merged-quality PR.
`)

	assembleFreshTestCatalog(t, ctx, ws, om)

	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: om})
	rows, err := s.LoadAgentTypes(ctx)
	if err != nil {
		t.Fatalf("LoadAgentTypes: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Name != "implementer" || r.Harness != "claude-code" || r.Model != "claude-opus-4-8" {
		t.Fatalf("row envelope = %+v", r)
	}
	if r.Owner != "team/platform" || r.Autonomy != "assist" {
		t.Fatalf("row owner/autonomy = %+v", r)
	}
	if len(r.Persona) == 0 || !contains(r.Persona, "One Ready task") {
		t.Fatalf("persona not resolved: %q", r.Persona)
	}
	// The mayAffect glob "shared" resolves to the shared component key edge.
	var sawShared bool
	for _, k := range r.MayAffect {
		if contains(k, "shared") {
			sawShared = true
		}
	}
	if !sawShared {
		t.Fatalf("mayAffect edge not resolved: %v", r.MayAffect)
	}
}

func TestLoadAgentTypes_AbsentStore(t *testing.T) {
	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: t.TempDir()})
	rows, err := s.LoadAgentTypes(context.Background())
	if err != nil || rows != nil {
		t.Fatalf("absent store: rows=%v err=%v", rows, err)
	}
}
