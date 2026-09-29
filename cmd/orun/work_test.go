package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// saas-work-gitops WG2: `orun work check` reads the declared tree, names
// every problem, and with --base answers the epic-status facts for the
// branch's task.

const testEpicYAML = `apiVersion: orun.io/v1
kind: Epic
metadata:
  name: saas-work-gitops
  key: WG
spec:
  title: "Work as code"
  state: started
  milestones:
    - name: "WG0"
      tasks: [WG-1]
    - name: "WG1"
      tasks: [WG-2]
`

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeWorkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range map[string]string{
		"intent.yaml":                                          "apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\nwork:\n  epics: work/epics\n  tasks: work/tasks\n",
		"work/epics/saas-work-gitops/epic.yaml":                testEpicYAML,
		"work/epics/saas-work-gitops/IMPLEMENTATION-STATUS.md": "# status\n",
		"work/tasks/WG-1.TaskContract.yaml":                    strings.Replace(testTaskDoc, "name: ENG-1", "name: WG-1", 1),
		"work/tasks/WG-2.TaskContract.yaml":                    strings.Replace(testTaskDoc, "name: ENG-1", "name: WG-2", 1),
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func runWorkCheck(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newWorkCheckCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestWorkCheckCleanTreeAndEpicFacts(t *testing.T) {
	dir := writeWorkRepo(t)
	out, err := runWorkCheck(t)
	if err != nil || !strings.Contains(out, "1 epic(s), 2 contract(s)") || !strings.Contains(out, "clean") {
		t.Fatalf("clean tree: %v\n%s", err, out)
	}
	// WG-2 is its milestone's only task: the branch closes it, and the diff
	// touches the status file.
	gitIn(t, dir, "checkout", "-q", "-b", "orun/WG-2-the-tree")
	if err := os.WriteFile(filepath.Join(dir, "work", "epics", "saas-work-gitops", "IMPLEMENTATION-STATUS.md"), []byte("# status\n| WG1 | done |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "WG1 done\n\nOrun-Task: WG-2")
	out, err = runWorkCheck(t, "--base", "main")
	if err != nil {
		t.Fatalf("with base: %v\n%s", err, out)
	}
	for _, want := range []string{"sits in saas-work-gitops / WG1", "closes the milestone: true; touches work/epics/saas-work-gitops/IMPLEMENTATION-STATUS.md: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWorkCheckNamesEveryProblem(t *testing.T) {
	dir := writeWorkRepo(t)
	// list a key with no contract, and add a reserved-prefix stray
	epic := strings.Replace(testEpicYAML, "tasks: [WG-2]", "tasks: [WG-2, WG-3]", 1)
	if err := os.WriteFile(filepath.Join(dir, "work", "epics", "saas-work-gitops", "epic.yaml"), []byte(epic), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work", "tasks", "WG-9.TaskContract.yaml"), []byte(strings.Replace(testTaskDoc, "name: ENG-1", "name: WG-9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWorkCheck(t)
	if err == nil {
		t.Fatalf("problems must exit non-zero:\n%s", out)
	}
	for _, want := range []string{
		"error work-manifest   work/epics/saas-work-gitops/epic.yaml: WG-3 is listed under \"WG1\" but work/tasks/WG-3.TaskContract.yaml does not exist",
		"error work-manifest   work/tasks/WG-9.TaskContract.yaml: WG-9 carries the prefix WG reserves but no milestone in work/epics/saas-work-gitops/epic.yaml lists it",
		"2 problem(s)",
	} {
		if !strings.Contains(out+err.Error(), want) {
			t.Errorf("missing %q in:\n%s\n%v", want, out, err)
		}
	}
}

func TestWorkCheckUndeclared(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out, err := runWorkCheck(t)
	if err != nil || !strings.Contains(out, "declares no work section") {
		t.Fatalf("undeclared: %v\n%s", err, out)
	}
}
