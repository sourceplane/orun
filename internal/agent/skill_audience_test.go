package agent

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	shipped "github.com/sourceplane/orun/agents"
	"github.com/sourceplane/orun/internal/remotestate"
)

// SD1 (saas-skill-driven-agents design §3): a box loads the skills whose
// `appliesTo` names its agent type, the `orunbase` router, and any skill
// that declares no audience — and keeps them out of the checkout's git.

func audienceView(name string, appliesTo ...string) remotestate.SkillView {
	v := skillView(name, "sha256:"+name, name, "# "+name+"\n")
	if appliesTo != nil {
		// The wire decodes a YAML list into []interface{}: decode one for real.
		raw, _ := json.Marshal(map[string]any{"appliesTo": appliesTo, "description": name})
		var fm map[string]interface{}
		_ = json.Unmarshal(raw, &fm)
		v.Frontmatter = fm
	}
	return v
}

func names(views []remotestate.SkillView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.Name)
	}
	sort.Strings(out)
	return out
}

func registry() []remotestate.SkillView {
	return []remotestate.SkillView{
		audienceView("orunbase", "initiative-owner"), // the router, whatever it says
		audienceView("task-branch-pr", "implementer", "initiative-owner", "orchestrator"),
		audienceView("milestone-loop", "initiative-owner", "implementer", "orchestrator"),
		audienceView("baselining", "bootstrapper", "orchestrator"),
		audienceView("planning", "work-agent", "initiative-owner", "orchestrator"),
		audienceView("our-house-style"), // an org skill with no audience
	}
}

func TestSkillsForAnImplementer(t *testing.T) {
	got := strings.Join(names(SkillsFor("implementer", registry())), ",")
	want := "milestone-loop,orunbase,our-house-style,task-branch-pr"
	if got != want {
		t.Fatalf("implementer loads %s, want %s", got, want)
	}
}

func TestSkillsForABootstrapper(t *testing.T) {
	got := strings.Join(names(SkillsFor("bootstrapper", registry())), ",")
	want := "baselining,orunbase,our-house-style"
	if got != want {
		t.Fatalf("bootstrapper loads %s, want %s", got, want)
	}
}

func TestSkillsForNoTypeKeepsEverything(t *testing.T) {
	if got := len(SkillsFor("", registry())); got != len(registry()) {
		t.Fatalf("type-less run loads %d skills, want all %d", got, len(registry()))
	}
}

func TestSkillAppliesToShapes(t *testing.T) {
	v := skillView("x", "sha256:x", "x", "x")
	v.Frontmatter = map[string]interface{}{"appliesTo": []string{" implementer ", ""}}
	if got, ok := SkillAppliesTo(v); !ok || len(got) != 1 || got[0] != "implementer" {
		t.Fatalf("[]string: %v %v", got, ok)
	}
	v.Frontmatter = map[string]interface{}{"appliesTo": "reviewer"}
	if got, ok := SkillAppliesTo(v); !ok || got[0] != "reviewer" {
		t.Fatalf("string: %v %v", got, ok)
	}
	v.Frontmatter = map[string]interface{}{"appliesTo": []interface{}{}}
	if _, ok := SkillAppliesTo(v); ok {
		t.Fatal("an empty list declares no audience")
	}
}

// Every shipped persona is a name the skills can address.
func TestShippedPersonasAreAgentTypeNames(t *testing.T) {
	known := map[string]bool{}
	for _, n := range AgentTypeNames {
		known[n] = true
	}
	entries, err := fs.ReadDir(shipped.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		seen++
		name := strings.TrimSuffix(e.Name(), ".md")
		if !known[name] {
			t.Errorf("agents/%s is not in AgentTypeNames %v", e.Name(), AgentTypeNames)
		}
	}
	if seen == 0 {
		t.Fatal("no shipped personas")
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func excludeLines(t *testing.T, repo string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func TestExcludeFromGitKeepsSkillsOutOfStatus(t *testing.T) {
	repo := gitRepo(t)
	skills := filepath.Join(repo, ".claude", "skills")
	if err := ExcludeFromGit(skills); err != nil {
		t.Fatal(err)
	}
	if err := ExcludeFromGit(skills); err != nil { // idempotent
		t.Fatal(err)
	}
	lines := excludeLines(t, repo)
	if len(lines) != 1 || lines[0] != "/.claude/skills/" {
		t.Fatalf("info/exclude = %v", lines)
	}
	if _, err := MaterializeSkills(skills, []remotestate.SkillView{audienceView("orunbase")}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("git status shows the skills:\n%s", out)
	}
}

func TestExcludeFromGitInASubdirectory(t *testing.T) {
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ExcludeFromGit(filepath.Join(repo, "app", ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	if lines := excludeLines(t, repo); len(lines) != 1 || lines[0] != "/app/.claude/skills/" {
		t.Fatalf("info/exclude = %v", lines)
	}
}

func TestExcludeFromGitOutsideARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	// Never let git climb into a repository that happens to hold TMPDIR.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if err := ExcludeFromGit(filepath.Join(dir, ".claude", "skills")); err != nil {
		t.Fatalf("outside a repository: %v", err)
	}
}
