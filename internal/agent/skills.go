package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
)

// skills.go — the driver's playbooks (orun-initiatives-v2 IS6, design §8):
// `orun agent run`/`serve` fetch the hosted skill set and write each
// revision as a NATIVE skill file the harness discovers on its own
// (<dir>/<name>/SKILL.md — the Claude Code project-skill layout), beside
// the MCP config the runtime already writes. The session records the
// exact revisions it materialized (SkillPin); the IS6 PR manifest names
// them, so a review can always re-read the playbook the agent ran under.
//
// Since saas-agent-skills SK2 a revision is a BUNDLE: SKILL.md plus the
// files under `references/` and `templates/`. Materialization writes them
// beside SKILL.md and keeps a ledger (`.orun-files`) of every path it
// wrote, so a later pull of a revision that dropped a file removes exactly
// that file — never anything the user put in the directory themselves.

// SkillPin names one skill revision a session ran under.
type SkillPin struct {
	Name string `json:"name"`
	Rev  string `json:"rev"`
}

// skillLedger is the file beside SKILL.md that lists the bundle paths the
// last materialization wrote. Pruning consults it, never the directory.
const skillLedger = ".orun-files"

// bundlePathRe is the registry's grammar for a bundle path — mirrored here
// so a hostile or broken response can never write outside the skill
// directory (`../`), a hidden file, or anything but the two bundle roots.
var bundlePathRe = regexp.MustCompile(`^(references|templates)(/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)

// MaterializeSkills writes each skill as <dir>/<name>/SKILL.md — a
// frontmatter block (name, description when present, and the pinned
// orun-rev) over the canonical body — plus the bundle's files beside it,
// and returns the pins, name order.
func MaterializeSkills(dir string, skills []remotestate.SkillView) ([]SkillPin, error) {
	pins := make([]SkillPin, 0, len(skills))
	for _, s := range skills {
		skillDir := filepath.Join(dir, s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return nil, fmt.Errorf("agent: skill dir %s: %w", s.Name, err)
		}
		var fm strings.Builder
		fm.WriteString("---\n")
		fm.WriteString("name: " + s.Name + "\n")
		if desc, ok := s.Frontmatter["description"].(string); ok && desc != "" {
			fm.WriteString("description: " + strings.ReplaceAll(desc, "\n", " ") + "\n")
		}
		// The pin travels IN the file too — a skill on disk always names
		// the revision it is, even away from the pins record.
		fm.WriteString("orun-rev: " + s.Rev + "\n")
		fm.WriteString("orun-source: " + s.Source + "\n")
		fm.WriteString("---\n\n")
		body := s.Body
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(fm.String()+body), 0o644); err != nil {
			return nil, fmt.Errorf("agent: skill %s: %w", s.Name, err)
		}
		if err := writeBundle(skillDir, s.Name, s.Files); err != nil {
			return nil, err
		}
		pins = append(pins, SkillPin{Name: s.Name, Rev: s.Rev})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Name < pins[j].Name })
	return pins, nil
}

// writeBundle writes the revision's files under the skill directory and
// prunes the paths the previous materialization wrote that this one did
// not. The ledger is the only authority for pruning: a file the user
// added by hand is never touched.
func writeBundle(skillDir, name string, files []remotestate.SkillFile) error {
	previous := readLedger(skillDir)
	written := make([]string, 0, len(files))
	for _, f := range files {
		if !bundlePathRe.MatchString(f.Path) {
			return fmt.Errorf("agent: skill %s: refusing bundle path %q (not under references/ or templates/)", name, f.Path)
		}
		full := filepath.Join(skillDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("agent: skill %s: %w", name, err)
		}
		if err := os.WriteFile(full, []byte(f.Body), 0o644); err != nil {
			return fmt.Errorf("agent: skill %s: %s: %w", name, f.Path, err)
		}
		written = append(written, f.Path)
	}
	sort.Strings(written)
	keep := make(map[string]bool, len(written))
	for _, p := range written {
		keep[p] = true
	}
	for _, p := range previous {
		if keep[p] || !bundlePathRe.MatchString(p) {
			continue
		}
		full := filepath.Join(skillDir, filepath.FromSlash(p))
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("agent: skill %s: pruning %s: %w", name, p, err)
		}
		// Empty bundle directories go with their last file.
		for d := filepath.Dir(full); d != skillDir; d = filepath.Dir(d) {
			if err := os.Remove(d); err != nil {
				break
			}
		}
	}
	ledger := filepath.Join(skillDir, skillLedger)
	if len(written) == 0 {
		if err := os.Remove(ledger); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("agent: skill %s: %w", name, err)
		}
		return nil
	}
	return os.WriteFile(ledger, []byte(strings.Join(written, "\n")+"\n"), 0o644)
}

func readLedger(skillDir string) []string {
	raw, err := os.ReadFile(filepath.Join(skillDir, skillLedger))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// WriteSkillPins records the materialized revisions beside the MCP config
// (skills.json) — the IS6 PR manifest's source of truth for what this
// session ran under.
func WriteSkillPins(path string, pins []SkillPin) error {
	b, err := json.MarshalIndent(map[string]any{"skills": pins}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("agent: skill pins dir: %w", err)
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// ReadLocalPins reads the revision each skill under dir is — the
// `orun-rev` its SKILL.md frontmatter carries — name order. A directory
// without a SKILL.md, or a SKILL.md without the pin (a skill the user
// wrote themselves), is skipped: it is not a registry copy.
func ReadLocalPins(dir string) ([]SkillPin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var pins []SkillPin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rev, ok := localSkillRev(filepath.Join(dir, e.Name(), "SKILL.md"))
		if !ok {
			continue
		}
		pins = append(pins, SkillPin{Name: e.Name(), Rev: rev})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Name < pins[j].Name })
	return pins, nil
}

// localSkillRev reads `orun-rev:` out of a SKILL.md's frontmatter block.
func localSkillRev(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.TrimSpace(line) != "---" {
				return "", false
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			return "", false
		}
		if strings.HasPrefix(line, "orun-rev:") {
			rev := strings.TrimSpace(strings.TrimPrefix(line, "orun-rev:"))
			return rev, strings.HasPrefix(rev, "sha256:")
		}
	}
	return "", false
}
