package taskfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/contract"
)

const fullDoc = `apiVersion: orun.io/v1
kind: TaskContract
metadata:
  name: ENG-42
spec:
  goal: ship the composer
  affects: [web-console-next]
  doneWhen:
    - one row per change
  gates: [tests]
  designRefs: [specs/x.md#s1]
  deps: [WEB-2]
  secrets: [STRIPE_TEST_*]
  envs: [dev]
`

func TestParseFullDocument(t *testing.T) {
	t.Parallel()
	doc, err := Parse("tasks/ENG-42.TaskContract.yaml", []byte(fullDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := doc.Contract
	if doc.Key != "ENG-42" || c.Goal != "ship the composer" || len(c.Secrets) != 1 || len(c.Envs) != 1 {
		t.Fatalf("roundtrip: %+v", doc)
	}
	if !c.Complete() {
		t.Fatal("full document parsed incomplete")
	}
	if c.GatesDefined {
		t.Fatal("non-empty gates must not set GatesDefined (the canonical form would double-declare)")
	}
}

func TestParseGatesDeclaration(t *testing.T) {
	t.Parallel()
	// gates: [] — an explicit declaration that merge alone finishes the work.
	declared := strings.Replace(fullDoc, "gates: [tests]", "gates: []", 1)
	doc, err := Parse("tasks/ENG-42.TaskContract.yaml", []byte(declared))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !doc.Contract.GatesDefined || len(doc.Contract.Gates) != 0 {
		t.Fatalf("explicit empty gates: %+v", doc.Contract)
	}
	if !doc.Contract.Complete() {
		t.Fatal("explicit empty gate set must count as declared")
	}

	// No gates key at all — gates unknown; the contract is not agent-ready.
	var lines []string
	for _, l := range strings.Split(fullDoc, "\n") {
		if !strings.Contains(l, "gates:") {
			lines = append(lines, l)
		}
	}
	doc, err = Parse("tasks/ENG-42.TaskContract.yaml", []byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Contract.GatesDefined || len(doc.Contract.Gates) != 0 {
		t.Fatalf("absent gates key: %+v", doc.Contract)
	}
	if doc.Contract.Complete() {
		t.Fatal("undeclared gates must keep the contract incomplete")
	}
}

func TestParseRefusals(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ path, body string }{
		"unknown field": {"tasks/ENG-42.TaskContract.yaml",
			strings.Replace(fullDoc, "secrets:", "secerts:", 1)},
		"wrong kind": {"tasks/ENG-42.TaskContract.yaml",
			strings.Replace(fullDoc, "kind: TaskContract", "kind: SecretPolicy", 1)},
		"wrong apiVersion": {"tasks/ENG-42.TaskContract.yaml",
			strings.Replace(fullDoc, "orun.io/v1", "orun.io/v2", 1)},
		"lowercase key": {"tasks/eng-42.TaskContract.yaml",
			strings.Replace(fullDoc, "name: ENG-42", "name: eng-42", 1)},
		"filename disagrees with name": {"tasks/WEB-1.TaskContract.yaml", fullDoc},
	}
	for name, tc := range cases {
		if _, err := Parse(tc.path, []byte(tc.body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFindForKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PathFor(root, "ENG-42"), []byte(fullDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := FindForKey(root, "ENG-42")
	if err != nil || doc == nil || doc.Key != "ENG-42" {
		t.Fatalf("find: %v %+v", err, doc)
	}
	// Absence is a legal state (no contract ⇒ no narrowing), not an error.
	doc, err = FindForKey(root, "WEB-9")
	if err != nil || doc != nil {
		t.Fatalf("absent doc: %v %+v", err, doc)
	}
	// A non-key never becomes a path segment — create feeds this the key
	// the CLOUD returned, and a hostile backend must not steer lookups.
	if _, err := FindForKey(root, "../../etc/passwd"); err == nil {
		t.Fatal("traversal-shaped key accepted")
	}
}

func TestMissing(t *testing.T) {
	t.Parallel()
	if m := Missing(&contract.Contract{Goal: "g"}); len(m) != 3 {
		t.Fatalf("missing on goal-only: %v", m)
	}
	full := &contract.Contract{Goal: "g", Affects: []string{"a"}, DoneWhen: []string{"d"}, GatesDefined: true}
	if m := Missing(full); len(m) != 0 {
		t.Fatalf("complete contract reported missing: %v", m)
	}
}

func TestParseTemplateIsUnbound(t *testing.T) {
	body := []byte("apiVersion: orun.io/v1\nkind: TaskContract\nspec:\n  goal: g\n  affects: [a]\n  doneWhen: [d]\n  gates: []\n")
	doc, err := ParseTemplate("anything.yaml", body)
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	if doc.Key != "" {
		t.Fatalf("template key = %q, want empty", doc.Key)
	}
	if !doc.Contract.GatesDefined || !doc.Contract.Complete() {
		t.Fatalf("template contract %+v: want gatesDefined + complete", doc.Contract)
	}
	// Parse (the bound form) refuses the same bytes: no name, no key.
	if _, err := Parse("anything.yaml", body); err == nil {
		t.Fatal("Parse accepted an unbound template")
	}
	// A bound name still parses through the template door, and a non-key
	// name is refused with the template hint.
	if d, err := ParseTemplate("x.yaml", append([]byte("metadata:\n  name: ENG-1\n"), body...)); err != nil || d.Key != "ENG-1" {
		t.Fatalf("bound template: %v %+v", err, d)
	}
	if _, err := ParseTemplate("x.yaml", append([]byte("metadata:\n  name: nope\n"), body...)); err == nil {
		t.Fatal("ParseTemplate accepted a non-key name")
	}
}

// saas-work-gitops WG-1: the declared tasks directory (intent.yaml
// `work.tasks`) is honoured by every lookup, and the finding-facing
// spelling is repo-relative with forward slashes.
func TestFindForKeyInDeclaredDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	titled := strings.Replace(fullDoc, "name: ENG-42", "name: ENG-42\n  title: Ship the composer", 1)
	if err := os.WriteFile(PathForIn(root, "work/tasks", "ENG-42"), []byte(titled), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := FindForKeyIn(root, "work/tasks", "ENG-42")
	if err != nil || doc == nil || doc.Key != "ENG-42" || doc.Title != "Ship the composer" {
		t.Fatalf("find in declared dir: %v %+v", err, doc)
	}
	// The default lookup does not see it: the layout is the repository's word.
	if doc, err := FindForKey(root, "ENG-42"); err != nil || doc != nil {
		t.Fatalf("default dir must not find a declared-dir doc: %v %+v", err, doc)
	}
	if got := RelPathIn("work/tasks", "ENG-42"); got != "work/tasks/ENG-42.TaskContract.yaml" {
		t.Fatalf("RelPathIn = %q", got)
	}
	if got := RelPathIn("", "ENG-42"); got != "tasks/ENG-42.TaskContract.yaml" {
		t.Fatalf("RelPathIn default = %q", got)
	}
	if _, err := FindForKeyIn(root, "work/tasks", "../x"); err == nil {
		t.Fatal("traversal-shaped key accepted")
	}
}
