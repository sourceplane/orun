package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/worklens"
)

func runWorkCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerWorkCommand(root)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestWorkImportDryRunJSON(t *testing.T) {
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("import --dry-run failed: %v\n%s", err, out)
	}
	var plan worklens.ImportPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("output is not a plan: %v\n%s", err, out)
	}
	if plan.Workspace != "ws_test" || len(plan.Specs) != 2 || len(plan.Tasks) != 2 {
		t.Fatalf("plan = %d specs, %d tasks, ws %q", len(plan.Specs), len(plan.Tasks), plan.Workspace)
	}
}

func TestWorkImportApplyNeedsBackend(t *testing.T) {
	// Apply (no --dry-run) must resolve a backend + workspace before any
	// write; in a bare test environment that resolution fails loudly rather
	// than silently doing nothing.
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test")
	if err == nil {
		t.Fatalf("apply succeeded without a backend:\n%s", out)
	}
}

func TestWorkImportHuman(t *testing.T) {
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"specs:     2", "tasks:     2", "demo-epic", "dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
