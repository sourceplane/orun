package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A console build never sent cirrus its workspace. The sandbox always knew it
// (ORUN_WORKSPACE), but the blueprint takes it as an input, so the product got
// `workspace: ws_SET_ME` and secret refs naming the repository. An input
// declared `from: workspace` is filled from the workspace the build runs in.
const workspaceBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: fixture
inputs:
  reponame:      { type: string, required: true }
  orunWorkspace: { type: string, default: "", from: workspace }
modules:
  - name: only
    mode: template
    files:
      "ws.txt": "workspace: {{ .orunWorkspace }}\n"
`

// buildWith runs the real `orun new` entry point over workspaceBlueprint and
// returns the placed ws.txt (what cirrus writes into intent.yaml).
func buildWith(t *testing.T, out, values string, resume bool) (string, error) {
	t.Helper()
	dir := t.TempDir()
	bp := filepath.Join(dir, "blueprint.yaml")
	if err := os.WriteFile(bp, []byte(workspaceBlueprint), 0o600); err != nil {
		t.Fatal(err)
	}
	vf := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(vf, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	pb, po, pv, ps, pr, pp, ph := scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet, scaffoldResume, scaffoldProgress, scaffoldRunHooks
	scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet = bp, out, vf, nil
	scaffoldResume, scaffoldProgress, scaffoldRunHooks = resume, "plain", false
	defer func() {
		scaffoldBlueprint, scaffoldOut, scaffoldValuesFile, scaffoldSet = pb, po, pv, ps
		scaffoldResume, scaffoldProgress, scaffoldRunHooks = pr, pp, ph
	}()
	if err := runScaffoldNew(context.Background()); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(out, "ws.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b)), nil
}

func TestRunScaffoldNewFillsTheWorkspaceTheBuildRunsIn(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "org_e3fae75f4f064af0b31395112e4bbfd3")
	t.Setenv("ORUN_ORG", "")
	got, err := buildWith(t, filepath.Join(t.TempDir(), "newne"), "reponame: newne\n", false)
	if err != nil {
		t.Fatalf("runScaffoldNew: %v", err)
	}
	if got != "workspace: org_e3fae75f4f064af0b31395112e4bbfd3" {
		t.Fatalf("ws.txt = %q, want the sandbox's workspace", got)
	}
}

// Set is set: an operator who names the workspace gets that one, whatever the
// shell they run in says.
func TestRunScaffoldNewKeepsAWorkspaceSomebodySet(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "org_from_the_shell")
	got, err := buildWith(t, filepath.Join(t.TempDir(), "newne"), "reponame: newne\norunWorkspace: ws_79BDXAZQ\n", false)
	if err != nil {
		t.Fatalf("runScaffoldNew: %v", err)
	}
	if got != "workspace: ws_79BDXAZQ" {
		t.Fatalf("ws.txt = %q, want the value that was set", got)
	}
}

// No workspace anywhere: the input's own default, exactly as before.
func TestRunScaffoldNewWithNoWorkspaceKeepsTheDefault(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "")
	t.Setenv("ORUN_ORG", "")
	got, err := buildWith(t, filepath.Join(t.TempDir(), "newne"), "reponame: newne\n", false)
	if err != nil {
		t.Fatalf("runScaffoldNew: %v", err)
	}
	if got != "workspace:" {
		t.Fatalf("ws.txt = %q, want the default (empty)", got)
	}
}

// A product records what it was built with. Resumed from a shell in another
// workspace, it must go on being built into the one it was started in — not
// be refused as a conflict, and not be moved.
func TestRunScaffoldNewResumeKeepsTheRecordedWorkspace(t *testing.T) {
	out := filepath.Join(t.TempDir(), "newne")
	t.Setenv("ORUN_WORKSPACE", "org_first")
	if _, err := buildWith(t, out, "reponame: newne\n", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	t.Setenv("ORUN_WORKSPACE", "org_elsewhere")
	got, err := buildWith(t, out, "reponame: newne\n", true)
	if err != nil {
		t.Fatalf("resume from another workspace's shell: %v", err)
	}
	if got != "workspace: org_first" {
		t.Fatalf("ws.txt = %q, want the recorded workspace", got)
	}
}

func TestBuildWorkspaceOrder(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := buildWorkspace(env(map[string]string{"ORUN_WORKSPACE": "ws_env", "ORUN_ORG": "org_env"})); got != "ws_env" {
		t.Errorf("ORUN_WORKSPACE first, got %q", got)
	}
	if got := buildWorkspace(env(map[string]string{"ORUN_WORKSPACE": " ", "ORUN_ORG": "org_env"})); got != "org_env" {
		t.Errorf("then ORUN_ORG, got %q", got)
	}
	if got := buildWorkspace(env(nil)); got != "" {
		t.Errorf("nothing anywhere is nothing, got %q", got)
	}
}
