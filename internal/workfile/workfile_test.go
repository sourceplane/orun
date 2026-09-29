package workfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

const goodEpic = `apiVersion: orun.io/v1
kind: Epic
metadata:
  name: saas-work-gitops
  key: WG
spec:
  title: "Work as code"
  summary: "One paragraph."
  state: started
  milestones:
    - name: "WG0 — the spec"
      exitCriteria: ["the doc set is on main"]
      tasks: [WG-1]
    - name: "WG1 — the tree"
      tasks: [WG-2, WG-3]
`

const contract = `apiVersion: orun.io/v1
kind: TaskContract
metadata:
  name: %s
spec:
  goal: do the thing
  affects: ["x/**"]
  doneWhen: ["it is done"]
  gates: []
`

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func layout() model.WorkLayout {
	return model.WorkLayout{Epics: "work/epics", Tasks: "work/tasks", Sync: "off"}
}

func TestParseEpicDefaults(t *testing.T) {
	t.Parallel()
	e, err := ParseEpic("work/epics/saas-work-gitops", "work/epics/saas-work-gitops/epic.yaml", []byte(goodEpic))
	if err != nil {
		t.Fatal(err)
	}
	if e.Slug != "saas-work-gitops" || e.Key != "WG" || e.State != "started" || e.Status != "IMPLEMENTATION-STATUS.md" || len(e.Docs) != 1 || e.Docs[0] != "*.md" {
		t.Fatalf("parsed: %+v", e)
	}
	if e.StatusPath() != "work/epics/saas-work-gitops/IMPLEMENTATION-STATUS.md" {
		t.Fatalf("status path %q", e.StatusPath())
	}
	if e.MilestoneOf("WG-3") != 1 || e.MilestoneOf("WG-9") != -1 {
		t.Fatal("MilestoneOf")
	}
	// state defaults to backlog when absent
	e2, err := ParseEpic("work/epics/saas-work-gitops", "p", []byte(strings.Replace(goodEpic, "  state: started\n", "", 1)))
	if err != nil || e2.State != "backlog" {
		t.Fatalf("default state: %v %+v", err, e2)
	}
}

func TestParseEpicRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, edit, with, want string }{
		{"unknown field", "  summary:", "  sumary:", "field sumary not found"},
		{"kind", "kind: Epic", "kind: Epci", "kind"},
		{"slug", "name: saas-work-gitops", "name: Saas_Work", "not a slug"},
		{"prefix", "key: WG", "key: wg", "key prefix"},
		{"state", "state: started", "state: shipping", "not one of"},
		{"foreign key", "tasks: [WG-1]", "tasks: [SK-1]", "outside this epic's prefix"},
		{"not a key", "tasks: [WG-1]", "tasks: [wg1]", "not a task key"},
		{"dup milestone", "WG1 — the tree", "WG0 — the spec", "listed twice"},
		{"title", `title: "Work as code"`, `title: ""`, "spec.title is required"},
	} {
		body := strings.Replace(goodEpic, c.edit, c.with, 1)
		_, err := ParseEpic("work/epics/saas-work-gitops", "epic.yaml", []byte(body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// the directory name must be the slug
	if _, err := ParseEpic("work/epics/other", "epic.yaml", []byte(goodEpic)); err == nil || !strings.Contains(err.Error(), "directory name") {
		t.Errorf("dir mismatch: %v", err)
	}
}

func TestLoadCleanTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "work/epics/saas-work-gitops/epic.yaml", goodEpic)
	write(t, root, "work/epics/saas-work-gitops/design.md", "# d\n")
	write(t, root, "work/epics/legacy-no-declaration/README.md", "# old\n")
	for _, k := range []string{"WG-1", "WG-2", "WG-3", "TSK-7"} {
		write(t, root, "work/tasks/"+k+".TaskContract.yaml", strings.ReplaceAll(contract, "%s", k))
	}
	tree, err := Load(root, layout())
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Problems) != 0 {
		t.Fatalf("clean tree has problems: %v", tree.Problems)
	}
	if len(tree.Epics) != 1 || len(tree.Contracts) != 4 || tree.Placement["WG-3"].Milestone != 1 {
		t.Fatalf("tree: %+v", tree)
	}
	if _, stray := tree.Placement["TSK-7"]; stray {
		t.Fatal("an unreserved-prefix contract is a stray, not a placement")
	}
	if p := tree.Prefixes(); len(p) != 1 || p[0] != "WG" {
		t.Fatalf("prefixes %v", p)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "work/epics/saas-work-gitops/epic.yaml", goodEpic)
	// a second epic reserving the same prefix, and listing a key the first lists
	write(t, root, "work/epics/zz-other/epic.yaml", strings.NewReplacer("name: saas-work-gitops", "name: zz-other", "tasks: [WG-2, WG-3]", "tasks: [WG-2]").Replace(goodEpic))
	// a malformed third
	write(t, root, "work/epics/broken/epic.yaml", "apiVersion: orun.io/v1\nkind: Epic\nmetadata:\n  name: broken\n")
	write(t, root, "work/tasks/WG-1.TaskContract.yaml", strings.ReplaceAll(contract, "%s", "WG-1"))
	// WG-2 missing; WG-3 present; WG-9 reserved but unlisted; bad file
	write(t, root, "work/tasks/WG-3.TaskContract.yaml", strings.ReplaceAll(contract, "%s", "WG-3"))
	write(t, root, "work/tasks/WG-9.TaskContract.yaml", strings.ReplaceAll(contract, "%s", "WG-9"))
	write(t, root, "work/tasks/WG-5.TaskContract.yaml", "not: yaml: [")
	tree, err := Load(root, layout())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tree.Problems, "\n")
	for _, want := range []string{
		"broken/epic.yaml: metadata.key",
		"zz-other/epic.yaml: metadata.key WG is already reserved by work/epics/saas-work-gitops/epic.yaml",
		"WG-2 is listed under \"WG1 — the tree\" but work/tasks/WG-2.TaskContract.yaml does not exist",
		"WG-9 carries the prefix WG reserves but no milestone in work/epics/saas-work-gitops/epic.yaml lists it",
		"work/tasks/WG-5.TaskContract.yaml",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
	// the duplicate-prefix epic's listings are not placed twice
	if tree.Placement["WG-1"].Epic.Slug != "saas-work-gitops" {
		t.Fatalf("placement of WG-1: %+v", tree.Placement["WG-1"])
	}
}

func TestLoadNoTree(t *testing.T) {
	t.Parallel()
	tree, err := Load(t.TempDir(), layout())
	if err != nil || len(tree.Epics) != 0 || len(tree.Problems) != 0 {
		t.Fatalf("empty root: %v %+v", err, tree)
	}
}
