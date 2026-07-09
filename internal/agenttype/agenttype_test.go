package agenttype

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"

	"github.com/sourceplane/orun/internal/nodes"
)

const validFile = `---
name: implementer
kind: agent-type
apiVersion: orun.io/v1
harness: claude-code
model: claude-opus-4-8
runtime:
  effort: high
  maxTokens: 64000
autonomyDefault: assist
tools:
  allow: [work_get, spec_get]
  ask: [contract_propose]
  deny: ["*"]
mayAffect: [billing-*]
secrets:
  use: ["secret://*/billing/*"]
owner: sourceplane/team/payments
extends: base-orun-literacy
---
# Implementer

One Ready task to a merged-quality PR.
`

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "implementer.md", validFile)
	d, issues := Load(p)
	if d == nil {
		t.Fatalf("load failed: %v", issues)
	}
	for _, i := range issues {
		if i.Level == "error" {
			t.Fatalf("unexpected error: %v", i)
		}
	}
	if d.Name != "implementer" || d.Harness != "claude-code" || d.Owner != "sourceplane/team/payments" {
		t.Fatalf("bad decl: %+v", d)
	}
	if d.Runtime == nil || d.Runtime.MaxTokens != 64000 || d.Runtime.Effort != "high" {
		t.Fatalf("runtime not parsed: %+v", d.Runtime)
	}
	if len(d.Tools.Allow) != 2 || d.Tools.Deny[0] != "*" {
		t.Fatalf("tools not parsed: %+v", d.Tools)
	}
	if d.Secrets == nil || len(d.Secrets.Use) != 1 {
		t.Fatalf("secrets not parsed: %+v", d.Secrets)
	}
	if !strings.HasPrefix(string(d.Body), "# Implementer") {
		t.Fatalf("body not verbatim: %q", d.Body[:20])
	}
}

func TestLoadRejections(t *testing.T) {
	dir := t.TempDir()

	// Unknown frontmatter key — closed schema.
	p := write(t, dir, "a.md", strings.Replace(validFile, "extends: base-orun-literacy", "surprise: true", 1))
	if d, issues := Load(p); d != nil || !hasError(issues) {
		t.Fatalf("unknown key accepted: %v", issues)
	}
	// Missing owner.
	p = write(t, dir, "b.md", strings.Replace(validFile, "owner: sourceplane/team/payments\n", "", 1))
	if d, issues := Load(p); d != nil || !hasError(issues) {
		t.Fatalf("ownerless accepted: %v", issues)
	}
	// Empty persona.
	idx := strings.Index(validFile, "# Implementer")
	p = write(t, dir, "c.md", validFile[:idx])
	if d, issues := Load(p); d != nil || !hasError(issues) {
		t.Fatalf("empty persona accepted: %v", issues)
	}
}

func TestLoadSkipsNonAgentFiles(t *testing.T) {
	dir := t.TempDir()
	// No frontmatter — the legacy free-form agent doc shape.
	p := write(t, dir, "notes.md", "# Just a doc\n\nno frontmatter here\n")
	d, issues := Load(p)
	if d != nil || hasError(issues) {
		t.Fatalf("plain doc should skip with a notice: %v", issues)
	}
	if len(issues) != 1 || issues[0].Level != "notice" {
		t.Fatalf("want one notice, got %v", issues)
	}
	// Frontmatter of another kind.
	p = write(t, dir, "other.md", "---\nkind: recipe\n---\nbody\n")
	if d, issues := Load(p); d != nil || hasError(issues) {
		t.Fatalf("other-kind doc should skip: %v", issues)
	}
}

func TestLoadDirDuplicatesAndOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "zeta.md", strings.Replace(validFile, "name: implementer", "name: zeta", 1))
	write(t, dir, "alpha.md", strings.Replace(validFile, "name: implementer", "name: alpha", 1))
	write(t, dir, "dup.md", strings.Replace(validFile, "name: implementer", "name: alpha", 1))
	decls, issues := LoadDir(dir)
	if len(decls) != 2 || decls[0].Name != "alpha" || decls[1].Name != "zeta" {
		t.Fatalf("want [alpha zeta], got %v (%v)", decls, issues)
	}
	if !hasError(issues) {
		t.Fatalf("duplicate name not flagged: %v", issues)
	}
	// Missing dir is not an error.
	if decls, issues := LoadDir(filepath.Join(dir, "nope")); decls != nil || issues != nil {
		t.Fatalf("missing dir should be empty: %v %v", decls, issues)
	}
}

func TestFrontmatterKeyOrderSealsIdentically(t *testing.T) {
	dir := t.TempDir()
	reordered := `---
kind: agent-type
owner: sourceplane/team/payments
name: implementer
model: claude-opus-4-8
harness: claude-code
apiVersion: orun.io/v1
runtime:
  maxTokens: 64000
  effort: high
autonomyDefault: assist
tools:
  deny: ["*"]
  allow: [work_get, spec_get]
  ask: [contract_propose]
mayAffect: [billing-*]
secrets:
  use: ["secret://*/billing/*"]
extends: base-orun-literacy
---
# Implementer

One Ready task to a merged-quality PR.
`
	d1, _ := Load(write(t, dir, "a.md", validFile))
	d2, _ := Load(write(t, dir, "b.md", reordered))
	if d1 == nil || d2 == nil {
		t.Fatal("load failed")
	}
	id1, err := nodes.AgentTypeID(objectstore.AlgoSHA256, d1.Snapshot(), d1.Body, "base-orun-literacy", []byte("lit"))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := nodes.AgentTypeID(objectstore.AlgoSHA256, d2.Snapshot(), d2.Body, "base-orun-literacy", []byte("lit"))
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("key order changed identity: %s vs %s", id1, id2)
	}
}
