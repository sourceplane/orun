package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/scaffold"
)

func declared() map[string]scaffold.InputSpec {
	return map[string]scaffold.InputSpec{
		"reponame":  {Type: scaffold.InputString, Required: true},
		"githuborg": {Type: scaffold.InputString, Required: true},
		"epicSlug":  {Type: scaffold.InputString, Default: "infra-baselining"},
	}
}

// THE CASE THAT COST SEVEN PROMPTS. A values file carrying `githubOrg` against
// a blueprint declaring `githuborg` differs only in case, so the operator
// cannot see it by reading — the error has to name the key they meant.
func TestRejectUndeclaredInputsNamesTheKeyYouMeant(t *testing.T) {
	err := rejectUndeclaredInputs(declared(), map[string]string{
		"reponame":  "acme-cloud",
		"githubOrg": "orundemo",
	})
	if err == nil {
		t.Fatal("rejectUndeclaredInputs = nil for a key the blueprint does not declare")
	}
	got := err.Error()
	for _, want := range []string{`unknown input "githubOrg"`, `did you mean "githuborg"?`, "Nothing has been written."} {
		if !strings.Contains(got, want) {
			t.Fatalf("error = %q, missing %q", got, want)
		}
	}
}

// Every declared key supplied: the check is not a second required-input gate,
// and an input left to its default is not "undeclared".
func TestRejectUndeclaredInputsPassesADeclaredSet(t *testing.T) {
	if err := rejectUndeclaredInputs(declared(), map[string]string{
		"reponame":  "acme-cloud",
		"githuborg": "orundemo",
	}); err != nil {
		t.Fatalf("rejectUndeclaredInputs = %v, want nil", err)
	}
	if err := rejectUndeclaredInputs(declared(), map[string]string{}); err != nil {
		t.Fatalf("rejectUndeclaredInputs(empty) = %v, want nil", err)
	}
}

// A key near nothing gets no invented suggestion — but the declared set is
// listed, which is the only thing that helps then.
func TestRejectUndeclaredInputsListsTheDeclaredSet(t *testing.T) {
	err := rejectUndeclaredInputs(declared(), map[string]string{"zzzzzzzzzzzz": "x"})
	if err == nil {
		t.Fatal("rejectUndeclaredInputs = nil for an undeclared key")
	}
	got := err.Error()
	if strings.Contains(got, "did you mean") {
		t.Fatalf("invented a suggestion for a key near nothing: %q", got)
	}
	if !strings.Contains(got, "declared inputs: epicSlug, githuborg, reponame") {
		t.Fatalf("error = %q, want the sorted declared set", got)
	}
}

// ORDERING IS THE WHOLE POINT. CollectInputs has always refused an undeclared
// key — deep in the render, after every missing input was asked for. This
// drives the real `orun new` entry point and asserts it refuses while still
// holding the values file: no store opened, no output directory written.
func TestRunScaffoldNewRefusesAnUndeclaredKeyBeforeBuilding(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "blueprint.yaml")
	if err := os.WriteFile(bp, []byte(`apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: fixture
inputs:
  reponame:  { type: string, required: true }
  githuborg: { type: string, required: true }
modules:
  - name: only
    mode: template
    files:
      "README.md": "{{ .reponame }} under {{ .githuborg }}\n"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	values := filepath.Join(dir, "acme.yaml")
	if err := os.WriteFile(values, []byte("reponame: acme-cloud\ngithubOrg: orundemo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "acme-cloud")

	swap := func(bpPath, outPath, valuesPath string) func() {
		pb, po, pv, ps := scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet
		scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet = bpPath, outPath, valuesPath, nil
		return func() {
			scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet = pb, po, pv, ps
		}
	}
	defer swap(bp, out, values)()

	err := runScaffoldNew(context.Background())
	if err == nil {
		t.Fatal("runScaffoldNew = nil with an undeclared key in the values file")
	}
	if !strings.Contains(err.Error(), `did you mean "githuborg"?`) {
		t.Fatalf("error = %q, want the suggestion from the early check", err.Error())
	}
	// The refusal must land before anything is placed.
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatalf("%s was created despite the refusal", out)
	}
}
