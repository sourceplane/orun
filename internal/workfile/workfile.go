// Package workfile reads the work tree a repository declares in intent.yaml
// (orun-cloud saas-work-gitops, design §1–§3): one directory per epic under
// `work.epics`, each with an epic.yaml — the declaration — and the task
// contracts under `work.tasks`. It parses the declaration strictly and
// judges the tree, so `orun work check` refuses a malformed tree before it
// merges and `orun work sync` never meets one on main.
package workfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/taskfile"
)

const (
	// EpicFile is the declaration's filename inside an epic directory.
	EpicFile = "epic.yaml"
	// DefaultStatusFile is the epic's status file when the declaration
	// names none — the file `orun work check --base` expects a
	// milestone-closing PR to touch.
	DefaultStatusFile = "IMPLEMENTATION-STATUS.md"

	wantAPIVersion = "orun.io/v1"
	wantKind       = "Epic"
)

var (
	slugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
	prefixRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}$`)
	keyRe    = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,7})-[0-9]+$`)
)

// States are the native epic states, the cloud's vocabulary (W1).
var States = []string{"backlog", "started", "paused", "completed", "canceled"}

// Epic is one parsed declaration.
type Epic struct {
	// Dir is the epic directory, repo-relative with forward slashes.
	Dir string
	// Path is the declaration file, repo-relative.
	Path string

	Slug       string
	Key        string
	Title      string
	Summary    string
	State      string
	Owner      string
	TargetDate string
	// Docs are the globs (relative to Dir) pushed as epic docs; ["*.md"]
	// when the declaration names none.
	Docs []string
	// Status is the status file, relative to Dir.
	Status     string
	Milestones []Milestone
}

// Milestone is one declared phase, in list order.
type Milestone struct {
	Name         string
	ExitCriteria []string
	Tasks        []string
}

// StatusPath is the status file, repo-relative.
func (e *Epic) StatusPath() string { return e.Dir + "/" + e.Status }

// MilestoneOf finds the milestone that lists a key; -1 when none does.
func (e *Epic) MilestoneOf(key string) int {
	for i, m := range e.Milestones {
		for _, k := range m.Tasks {
			if k == key {
				return i
			}
		}
	}
	return -1
}

type rawEpic struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
		Key  string `yaml:"key"`
	} `yaml:"metadata"`
	Spec struct {
		Title      string   `yaml:"title"`
		Summary    string   `yaml:"summary"`
		State      string   `yaml:"state"`
		Owner      string   `yaml:"owner"`
		TargetDate string   `yaml:"targetDate"`
		Docs       []string `yaml:"docs"`
		Status     string   `yaml:"status"`
		Milestones []struct {
			Name         string   `yaml:"name"`
			ExitCriteria []string `yaml:"exitCriteria"`
			Tasks        []string `yaml:"tasks"`
		} `yaml:"milestones"`
	} `yaml:"spec"`
}

// ParseEpic strictly decodes one declaration (unknown fields refused, the
// way a task contract is) and applies the per-file rules of design §2.
// dir is the epic directory, repo-relative; path the file, for messages.
func ParseEpic(dir, path string, body []byte) (*Epic, error) {
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var raw rawEpic
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if raw.APIVersion != wantAPIVersion {
		return nil, fmt.Errorf("%s: apiVersion %q (want %s)", path, raw.APIVersion, wantAPIVersion)
	}
	if raw.Kind != wantKind {
		return nil, fmt.Errorf("%s: kind %q (want %s)", path, raw.Kind, wantKind)
	}
	e := &Epic{Dir: dir, Path: path,
		Slug: strings.TrimSpace(raw.Metadata.Name), Key: strings.TrimSpace(raw.Metadata.Key),
		Title: strings.TrimSpace(raw.Spec.Title), Summary: strings.TrimSpace(raw.Spec.Summary),
		State: strings.ToLower(strings.TrimSpace(raw.Spec.State)), Owner: strings.TrimSpace(raw.Spec.Owner),
		TargetDate: strings.TrimSpace(raw.Spec.TargetDate), Docs: raw.Spec.Docs, Status: strings.TrimSpace(raw.Spec.Status)}
	if !slugRe.MatchString(e.Slug) {
		return nil, fmt.Errorf("%s: metadata.name %q is not a slug ([a-z0-9-], up to 80)", path, e.Slug)
	}
	if base := filepath.Base(dir); base != e.Slug {
		return nil, fmt.Errorf("%s: metadata.name %q must equal the directory name %q", path, e.Slug, base)
	}
	if !prefixRe.MatchString(e.Key) {
		return nil, fmt.Errorf("%s: metadata.key %q is not a key prefix ([A-Z][A-Z0-9]{1,7})", path, e.Key)
	}
	if e.Title == "" {
		return nil, fmt.Errorf("%s: spec.title is required", path)
	}
	if e.State == "" {
		e.State = "backlog"
	}
	if !contains(States, e.State) {
		return nil, fmt.Errorf("%s: spec.state %q is not one of %s", path, raw.Spec.State, strings.Join(States, ", "))
	}
	if len(e.Docs) == 0 {
		e.Docs = []string{"*.md"}
	}
	if e.Status == "" {
		e.Status = DefaultStatusFile
	}
	if strings.Contains(e.Status, "/") || strings.Contains(e.Status, "\\") {
		return nil, fmt.Errorf("%s: spec.status %q must be a file in the epic directory", path, e.Status)
	}
	seen := map[string]bool{}
	for i, m := range raw.Spec.Milestones {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			return nil, fmt.Errorf("%s: spec.milestones[%d].name is required", path, i)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s: milestone %q is listed twice", path, name)
		}
		seen[name] = true
		ms := Milestone{Name: name, ExitCriteria: m.ExitCriteria}
		for _, k := range m.Tasks {
			k = strings.TrimSpace(k)
			sub := keyRe.FindStringSubmatch(k)
			if sub == nil {
				return nil, fmt.Errorf("%s: milestone %q lists %q, which is not a task key", path, name, k)
			}
			if sub[1] != e.Key {
				return nil, fmt.Errorf("%s: milestone %q lists %s, outside this epic's prefix %s-", path, name, k, e.Key)
			}
			ms.Tasks = append(ms.Tasks, k)
		}
		e.Milestones = append(e.Milestones, ms)
	}
	return e, nil
}

// Placement is where a key sits in the tree.
type Placement struct {
	Epic      *Epic
	Milestone int
}

// Tree is the loaded work tree.
type Tree struct {
	Layout model.WorkLayout
	Epics  []*Epic
	// Contracts are the parsed contract documents by key (every file in the
	// tasks directory, listed or not).
	Contracts map[string]*taskfile.Document
	// Placement is the milestone that lists each key.
	Placement map[string]Placement
	// Problems are the cross-file rule violations (design §2), each a
	// sentence naming the file. Per-file parse errors are Problems too, so
	// one pass reports everything a PR must fix.
	Problems []string
}

// Load reads the tree under root with the declared layout. It returns an
// error only when the directories cannot be read; a malformed tree is a
// Tree with Problems.
func Load(root string, layout model.WorkLayout) (*Tree, error) {
	t := &Tree{Layout: layout, Contracts: map[string]*taskfile.Document{}, Placement: map[string]Placement{}}
	epicsDir := filepath.Join(root, filepath.FromSlash(layout.Epics))
	entries, err := os.ReadDir(epicsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("workfile: %w", err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := layout.Epics + "/" + ent.Name()
		path := dir + "/" + EpicFile
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			continue // a directory with no declaration is not an epic (a doc set that predates WG)
		}
		e, err := ParseEpic(dir, path, body)
		if err != nil {
			t.Problems = append(t.Problems, err.Error())
			continue
		}
		t.Epics = append(t.Epics, e)
	}
	sort.Slice(t.Epics, func(i, j int) bool { return t.Epics[i].Slug < t.Epics[j].Slug })

	tasksDir := filepath.Join(root, filepath.FromSlash(layout.Tasks))
	docs, err := os.ReadDir(tasksDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("workfile: %w", err)
	}
	for _, ent := range docs {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), taskfile.Suffix) {
			continue
		}
		rel := layout.Tasks + "/" + ent.Name()
		body, err := os.ReadFile(filepath.Join(tasksDir, ent.Name()))
		if err != nil {
			t.Problems = append(t.Problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		doc, err := taskfile.Parse(rel, body)
		if err != nil {
			t.Problems = append(t.Problems, err.Error())
			continue
		}
		t.Contracts[doc.Key] = doc
	}
	t.judge()
	return t, nil
}

// judge applies the cross-file rules.
func (t *Tree) judge() {
	prefixes := map[string]*Epic{}
	for _, e := range t.Epics {
		if other, dup := prefixes[e.Key]; dup {
			t.Problems = append(t.Problems, fmt.Sprintf("%s: metadata.key %s is already reserved by %s", e.Path, e.Key, other.Path))
			continue
		}
		prefixes[e.Key] = e
	}
	for _, e := range t.Epics {
		for i, m := range e.Milestones {
			for _, k := range m.Tasks {
				if prev, listed := t.Placement[k]; listed {
					t.Problems = append(t.Problems, fmt.Sprintf("%s: %s is listed under %q and already under %q in %s", e.Path, k, m.Name, prev.Epic.Milestones[prev.Milestone].Name, prev.Epic.Path))
					continue
				}
				t.Placement[k] = Placement{Epic: e, Milestone: i}
				if _, ok := t.Contracts[k]; !ok {
					t.Problems = append(t.Problems, fmt.Sprintf("%s: %s is listed under %q but %s does not exist", e.Path, k, m.Name, taskfile.RelPathIn(t.Layout.Tasks, k)))
				}
			}
		}
	}
	keys := make([]string, 0, len(t.Contracts))
	for k := range t.Contracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, listed := t.Placement[k]; listed {
			continue
		}
		prefix := keyRe.FindStringSubmatch(k)[1]
		if e, reserved := prefixes[prefix]; reserved {
			t.Problems = append(t.Problems, fmt.Sprintf("%s: %s carries the prefix %s reserves but no milestone in %s lists it", t.Contracts[k].Path, k, e.Key, e.Path))
		}
	}
}

// Prefixes lists the reserved key prefixes, sorted.
func (t *Tree) Prefixes() []string {
	out := make([]string, 0, len(t.Epics))
	for _, e := range t.Epics {
		out = append(out, e.Key)
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
