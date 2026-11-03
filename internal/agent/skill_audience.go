package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
)

// skill_audience.go — which skills a box loads (saas-skill-driven-agents
// SD1, design §3). A skill's `appliesTo` is the authority: it lives with the
// skill in the registry, an org that shadows a skill sets its own, and a new
// skill reaches the right boxes without an orun release. The persona never
// carries a second list.

// AgentTypeNames is the closed vocabulary `appliesTo` speaks — every agent
// that loads Orunbase skills, by the name its skills address it with. The
// same list is pinned in orun-cloud (packages/db/src/skills), so a skill
// cannot name an agent this runtime does not know, and the shipped personas
// (agents/*.md) must each be one of them.
var AgentTypeNames = []string{
	"work-agent",       // orun.work's planner (a worker, not a box)
	"implementer",      // a coding session on one task — orun.work's builder
	"bootstrapper",     // an unattended baseline bootstrap
	"initiative-owner", // a person's agent shaping an epic in a repository
	"orchestrator",     // an agent sequencing an epic's tasks
	"reviewer",         // an agent reviewing a pull request
}

// RouterSkill is loaded by every agent whatever its `appliesTo`: it is the
// entry point that routes to every other playbook by name.
const RouterSkill = "orunbase"

// SkillAppliesTo reads a revision's `appliesTo` frontmatter. ok is false
// when the skill declares none (an org skill written before audiences).
func SkillAppliesTo(s remotestate.SkillView) (names []string, ok bool) {
	raw, present := s.Frontmatter["appliesTo"]
	if !present || raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, x := range v {
			if n, isStr := x.(string); isStr && strings.TrimSpace(n) != "" {
				names = append(names, strings.TrimSpace(n))
			}
		}
	case []string:
		for _, n := range v {
			if strings.TrimSpace(n) != "" {
				names = append(names, strings.TrimSpace(n))
			}
		}
	case string:
		if strings.TrimSpace(v) != "" {
			names = []string{strings.TrimSpace(v)}
		}
	}
	return names, len(names) > 0
}

// SkillsFor keeps the skills an agent of this type loads: the router, every
// skill whose `appliesTo` names the type, and every skill that declares no
// audience. An empty type keeps everything (a type-less run had no audience
// to match, and loaded the whole registry before audiences existed).
func SkillsFor(agentType string, skills []remotestate.SkillView) []remotestate.SkillView {
	agentType = strings.TrimSpace(agentType)
	if agentType == "" {
		return skills
	}
	out := make([]remotestate.SkillView, 0, len(skills))
	for _, s := range skills {
		if s.Name == RouterSkill {
			out = append(out, s)
			continue
		}
		names, declared := SkillAppliesTo(s)
		if !declared {
			out = append(out, s)
			continue
		}
		for _, n := range names {
			if n == agentType {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// ExcludeFromGit keeps a directory the runtime writes into a checkout out of
// git (design §3, SD-3): it appends the directory, relative to the work
// tree's root, to the repository's local `info/exclude` — never committed,
// never untracking a file the repository already tracks. Not a git
// checkout, or no git on PATH: nothing to do, no error.
func ExcludeFromGit(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	// The nearest existing ancestor runs git: the directory itself may not
	// exist yet (the runtime excludes it before it writes).
	probe := abs
	for {
		if st, statErr := os.Stat(probe); statErr == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return nil
		}
		probe = parent
	}
	top, err := gitOut(probe, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return nil
	}
	excludePath, err := gitOut(probe, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil || excludePath == "" {
		// git < 2.31 has no --path-format: the path is relative to probe.
		if excludePath, err = gitOut(probe, "rev-parse", "--git-path", "info/exclude"); err != nil || excludePath == "" {
			return nil
		}
		if !filepath.IsAbs(excludePath) {
			excludePath = filepath.Join(probe, excludePath)
		}
	}
	// Both sides through EvalSymlinks: a temp dir behind a symlink (macOS
	// /var → /private/var) would otherwise read as outside the tree.
	realTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		realTop = top
	}
	realProbe, err := filepath.EvalSymlinks(probe)
	if err != nil {
		realProbe = probe
	}
	restRel, err := filepath.Rel(probe, abs)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realTop, filepath.Join(realProbe, restRel))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	line := "/" + filepath.ToSlash(rel) + "/"
	existing, _ := os.ReadFile(excludePath)
	for _, l := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("agent: git exclude dir: %w", err)
	}
	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteString("# written by orun: materialized skills (never committed)\n")
	buf.WriteString(line + "\n")
	return os.WriteFile(excludePath, buf.Bytes(), 0o644)
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
