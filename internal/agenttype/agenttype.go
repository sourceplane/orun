// Package agenttype loads agent-type definitions from agents/*.md files
// (specs/orun-agents/agent-type-format.md): YAML capability frontmatter (the
// policy contract, closed schema) + a markdown persona body (the character,
// stored verbatim). A loaded declaration seals into a nodes.AgentTypeSnapshot
// and projects into the catalog as an AgentType entity.
package agenttype

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/nodes"
)

// FileKind is the frontmatter `kind` that marks a file as an agent type. A
// file under agents/ without this kind is not an agent type (e.g. a legacy
// free-form agent doc) and is skipped with a notice, never an error — the
// graceful-adoption path.
const FileKind = "agent-type"

// Decl is one parsed agents/<name>.md: the capability frontmatter plus the
// verbatim persona body.
type Decl struct {
	Name            string
	Harness         string
	Model           string
	Runtime         *nodes.AgentRuntime
	AutonomyDefault string
	Tools           nodes.AgentToolPolicy
	MayAffect       []string
	Secrets         *nodes.AgentSecrets
	Owner           string
	Extends         string
	Body            []byte
	Path            string
}

// Issue is one lint finding. Level is "error" (blocks seal) or "notice".
type Issue struct {
	Path    string
	Level   string
	Message string
}

func (i Issue) String() string { return i.Path + ": " + i.Level + ": " + i.Message }

// frontmatter is the closed YAML schema. KnownFields(true) makes unknown keys
// a hard error (agent-type-format.md §5: forward-compat via apiVersion bumps,
// never silent-accept).
type frontmatter struct {
	Name       string `yaml:"name"`
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Harness    string `yaml:"harness"`
	Model      string `yaml:"model"`
	Runtime    *struct {
		Effort        string `yaml:"effort"`
		Temperature   string `yaml:"temperature"`
		MaxTokens     int    `yaml:"maxTokens"`
		ContextBudget int    `yaml:"contextBudget"`
	} `yaml:"runtime"`
	AutonomyDefault string `yaml:"autonomyDefault"`
	Tools           struct {
		Allow []string `yaml:"allow"`
		Ask   []string `yaml:"ask"`
		Deny  []string `yaml:"deny"`
	} `yaml:"tools"`
	MayAffect []string `yaml:"mayAffect"`
	Secrets   *struct {
		Use []string `yaml:"use"`
	} `yaml:"secrets"`
	Owner   string `yaml:"owner"`
	Extends string `yaml:"extends"`
}

var fmDelim = []byte("---")

// split separates YAML frontmatter from the body. ok=false when the file has
// no frontmatter block at all.
func split(raw []byte) (fm, body []byte, ok bool) {
	if !bytes.HasPrefix(raw, fmDelim) {
		return nil, nil, false
	}
	rest := raw[len(fmDelim):]
	if len(rest) > 0 && rest[0] == '\r' {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '\n' {
		return nil, nil, false
	}
	rest = rest[1:]
	end := bytes.Index(rest, append([]byte("\n"), fmDelim...))
	if end < 0 {
		return nil, nil, false
	}
	fm = rest[:end]
	body = rest[end+1+len(fmDelim):]
	body = bytes.TrimPrefix(body, []byte("\r"))
	body = bytes.TrimPrefix(body, []byte("\n"))
	return fm, body, true
}

// Load parses one agent-type file. A file without frontmatter, or whose
// frontmatter kind is not "agent-type", returns (nil, notice): not an agent
// type, not an error.
func Load(path string) (*Decl, []Issue) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, []Issue{{Path: path, Level: "error", Message: err.Error()}}
	}
	fmBytes, body, ok := split(raw)
	if !ok {
		return nil, []Issue{{Path: path, Level: "notice", Message: "no frontmatter — not an agent type, skipped"}}
	}

	// Kind gate first (loose decode): only kind: agent-type files bind to the
	// closed schema; anything else is skipped.
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(fmBytes, &probe); err != nil {
		return nil, []Issue{{Path: path, Level: "error", Message: "frontmatter: " + err.Error()}}
	}
	if probe.Kind != FileKind {
		return nil, []Issue{{Path: path, Level: "notice", Message: fmt.Sprintf("kind %q — not an agent type, skipped", probe.Kind)}}
	}

	var f frontmatter
	dec := yaml.NewDecoder(bytes.NewReader(fmBytes))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, []Issue{{Path: path, Level: "error", Message: "frontmatter (closed schema): " + err.Error()}}
	}

	var issues []Issue
	errf := func(format string, args ...any) {
		issues = append(issues, Issue{Path: path, Level: "error", Message: fmt.Sprintf(format, args...)})
	}
	if f.APIVersion != "orun.io/v1" {
		errf("apiVersion %q (want orun.io/v1)", f.APIVersion)
	}
	if f.Name == "" {
		errf("name missing")
	}
	if f.Harness == "" {
		errf("harness missing")
	}
	if f.Model == "" {
		errf("model missing")
	}
	if f.Owner == "" {
		errf("owner missing (a responsible owner is mandatory)")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		errf("persona body empty (an agent type with no character is a config error)")
	}
	if base := strings.TrimSuffix(filepath.Base(path), ".md"); f.Name != "" && base != f.Name {
		issues = append(issues, Issue{Path: path, Level: "notice",
			Message: fmt.Sprintf("name %q differs from filename %q", f.Name, base)})
	}
	if len(issues) > 0 && hasError(issues) {
		return nil, issues
	}

	d := &Decl{
		Name:            f.Name,
		Harness:         f.Harness,
		Model:           f.Model,
		AutonomyDefault: f.AutonomyDefault,
		Tools:           nodes.AgentToolPolicy{Allow: f.Tools.Allow, Ask: f.Tools.Ask, Deny: f.Tools.Deny},
		MayAffect:       f.MayAffect,
		Owner:           f.Owner,
		Extends:         f.Extends,
		Body:            body,
		Path:            path,
	}
	if f.Runtime != nil {
		d.Runtime = &nodes.AgentRuntime{
			Effort:        f.Runtime.Effort,
			Temperature:   f.Runtime.Temperature,
			MaxTokens:     f.Runtime.MaxTokens,
			ContextBudget: f.Runtime.ContextBudget,
		}
	}
	if f.Secrets != nil {
		d.Secrets = &nodes.AgentSecrets{Use: f.Secrets.Use}
	}

	// Validate through the node schema so the loader and the seal agree on
	// one rulebook (a placeholder bodyRef stands in for the not-yet-written
	// persona blob; extends validates at seal time when the literacy pin is
	// resolved).
	probeSnap := d.Snapshot()
	probeSnap.BodyRef = "sha256:" + strings.Repeat("0", 64)
	probeSnap.Extends = ""
	if err := probeSnap.Validate(); err != nil {
		errf("%v", err)
		return nil, issues
	}
	return d, issues
}

func hasError(issues []Issue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}

// Snapshot maps the declaration to its node record (bodyRef/extends stamped
// at assembly).
func (d *Decl) Snapshot() nodes.AgentTypeSnapshot {
	return nodes.AgentTypeSnapshot{
		Kind:            nodes.KindAgentTypeSnapshot,
		APIVersion:      "orun.io/v1",
		Name:            d.Name,
		Harness:         d.Harness,
		Model:           d.Model,
		Runtime:         d.Runtime,
		AutonomyDefault: d.AutonomyDefault,
		Tools:           d.Tools,
		MayAffect:       d.MayAffect,
		Secrets:         d.Secrets,
		Owner:           d.Owner,
		Extends:         d.Extends,
	}
}

// LoadDir loads every *.md under dir (non-recursive), returning declarations
// sorted by name plus all issues. A missing dir is not an error — a workspace
// without agents/ simply defines no agent types.
func LoadDir(dir string) ([]*Decl, []Issue) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Issue{{Path: dir, Level: "error", Message: err.Error()}}
	}
	var decls []*Decl
	var issues []Issue
	seen := map[string]string{} // name → path
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		d, is := Load(p)
		issues = append(issues, is...)
		if d == nil {
			continue
		}
		if prev, dup := seen[d.Name]; dup {
			issues = append(issues, Issue{Path: p, Level: "error",
				Message: fmt.Sprintf("duplicate agent-type name %q (also %s)", d.Name, prev)})
			continue
		}
		seen[d.Name] = p
		decls = append(decls, d)
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].Name < decls[j].Name })
	return decls, issues
}
