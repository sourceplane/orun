package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// IS6: materialization writes NATIVE skill files — the harness discovers
// them on its own — with the pinned revision IN the file, and the pins
// record what this session ran under (the PR manifest's source).

func skillView(name, rev, desc, body string) remotestate.SkillView {
	v := remotestate.SkillView{Body: body}
	v.Name = name
	v.Rev = rev
	v.Source = "default"
	v.Frontmatter = map[string]interface{}{"description": desc}
	return v
}

func TestMaterializeSkills(t *testing.T) {
	dir := t.TempDir()
	pins, err := MaterializeSkills(dir, []remotestate.SkillView{
		skillView("pr-provenance", "sha256:bb", "The pen.", "# PR provenance\n\nBranch grammar."),
		skillView("milestone-loop", "sha256:aa", "One milestone in flight.", "# The milestone loop\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pins come back name-ordered — a stable manifest, whatever the fetch order.
	if len(pins) != 2 || pins[0].Name != "milestone-loop" || pins[1].Rev != "sha256:bb" {
		t.Fatalf("pins = %+v", pins)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "milestone-loop", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"name: milestone-loop",
		"description: One milestone in flight.",
		"orun-rev: sha256:aa", // the pin travels IN the file
		"# The milestone loop",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("SKILL.md lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "---\n") {
		t.Error("SKILL.md must open with the frontmatter block")
	}
	// A body without a trailing newline gains one — file hygiene.
	prBody, _ := os.ReadFile(filepath.Join(dir, "pr-provenance", "SKILL.md"))
	if !strings.HasSuffix(string(prBody), "\n") {
		t.Error("materialized body must end with a newline")
	}

	pinsPath := filepath.Join(t.TempDir(), "agent-mcp", "skills.json")
	if err := WriteSkillPins(pinsPath, pins); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Skills []SkillPin `json:"skills"`
	}
	b, _ := os.ReadFile(pinsPath)
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Skills) != 2 || recorded.Skills[0].Rev != "sha256:aa" {
		t.Fatalf("recorded pins = %+v", recorded.Skills)
	}
}

// SK2 (saas-agent-skills): a revision is a bundle. Materialization writes
// the files beside SKILL.md, refuses a path outside the bundle roots, and
// prunes only what its own ledger says it wrote.
func TestMaterializeSkillsBundle(t *testing.T) {
	dir := t.TempDir()
	v := skillView("software-factory", "sha256:aa", "Build it.", "# Software factory\n")
	v.Files = []remotestate.SkillFile{
		{Path: "templates/README.md", Body: "# <title>\n"},
		{Path: "references/why.md", Body: "Because.\n"},
		{Path: "references/deep/more.md", Body: "More.\n"},
	}
	if _, err := MaterializeSkills(dir, []remotestate.SkillView{v}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SKILL.md", "templates/README.md", "references/why.md", "references/deep/more.md", ".orun-files"} {
		if _, err := os.Stat(filepath.Join(dir, "software-factory", filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// A file the user placed there themselves is not the ledger's to prune.
	mine := filepath.Join(dir, "software-factory", "references", "mine.md")
	if err := os.WriteFile(mine, []byte("hands off\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The next revision drops two files: exactly those go, the empty
	// directory with them, the user's file stays.
	v.Rev = "sha256:bb"
	v.Files = v.Files[:1]
	if _, err := MaterializeSkills(dir, []remotestate.SkillView{v}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "software-factory", "references", "why.md")); !os.IsNotExist(err) {
		t.Error("references/why.md should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "software-factory", "references", "deep")); !os.IsNotExist(err) {
		t.Error("references/deep/ should have gone with its last file")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("references/mine.md was not the ledger's to remove")
	}
	ledger, _ := os.ReadFile(filepath.Join(dir, "software-factory", ".orun-files"))
	if strings.TrimSpace(string(ledger)) != "templates/README.md" {
		t.Errorf("ledger = %q", ledger)
	}

	// A body-only revision clears the ledger.
	v.Rev = "sha256:cc"
	v.Files = nil
	if _, err := MaterializeSkills(dir, []remotestate.SkillView{v}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "software-factory", ".orun-files")); !os.IsNotExist(err) {
		t.Error("a body-only revision leaves no ledger")
	}

	// A path outside the bundle roots is refused before anything is written.
	bad := skillView("evil", "sha256:dd", "", "# Evil\n")
	bad.Files = []remotestate.SkillFile{{Path: "../../.ssh/authorized_keys", Body: "x"}}
	if _, err := MaterializeSkills(dir, []remotestate.SkillView{bad}); err == nil || !strings.Contains(err.Error(), "refusing bundle path") {
		t.Errorf("expected a refusal, got %v", err)
	}
}

// SK4/SK6: `orun skills status` reads the pin each local SKILL.md carries.
func TestReadLocalPins(t *testing.T) {
	dir := t.TempDir()
	if _, err := MaterializeSkills(dir, []remotestate.SkillView{
		skillView("orunbase", "sha256:11", "Entry point.", "# Orunbase\n"),
		skillView("mcp-tools", "sha256:22", "Tools.", "# Tools\n"),
	}); err != nil {
		t.Fatal(err)
	}
	// A hand-written skill (no orun-rev) and a stray file are not registry copies.
	os.MkdirAll(filepath.Join(dir, "my-own"), 0o755)
	os.WriteFile(filepath.Join(dir, "my-own", "SKILL.md"), []byte("---\nname: my-own\n---\n# Mine\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a skill"), 0o644)

	pins, err := ReadLocalPins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 2 || pins[0].Name != "mcp-tools" || pins[0].Rev != "sha256:22" || pins[1].Name != "orunbase" {
		t.Fatalf("pins = %+v", pins)
	}
	if got, _ := ReadLocalPins(filepath.Join(dir, "nope")); got != nil {
		t.Errorf("a missing dir reads as no pins, got %+v", got)
	}
}
