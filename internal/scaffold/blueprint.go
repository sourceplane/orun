package scaffold

import (
	"fmt"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// APIVersion and Kind are the fixed envelope of a Blueprint document.
const (
	BlueprintAPIVersion = "orun.dev/v1"
	BlueprintKind       = "Blueprint"
)

// Blueprint is the single typed document (kind: Blueprint) that describes a
// scaffold at any scale. Its three substantive sections — inputs, sources,
// modules — are scale-independent: a one-module blueprint with no sources IS
// the single-service scaffolder; the same schema with a source and many
// modules IS the product instantiator (design §3).
type Blueprint struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   BlueprintMetadata `yaml:"metadata" json:"metadata"`

	// Inputs is the typed contract.inputs schema (SC7), verbatim. One schema
	// powers prompts, flags, and a portal form.
	Inputs map[string]InputSpec `yaml:"inputs,omitempty" json:"inputs,omitempty"`

	// Sources declares where module content is fetched from. Zero sources ⇒
	// pure inline templates (single-component scale). One ⇒ a baseline fork.
	// A list ⇒ compose from several baselines.
	Sources []SourceSpec `yaml:"sources,omitempty" json:"sources,omitempty"`

	// Modules is the atom of scaffolding: one module = a component, many = a
	// repo. Placed in the DAG order computed over declared edges (design §6).
	Modules []Module `yaml:"modules" json:"modules"`

	// Hooks declares ecosystem-specific post-steps run outside the sandbox
	// (design §12). orun executes the declared argv; it never internalizes the
	// tools they name.
	Hooks Hooks `yaml:"hooks,omitempty" json:"hooks,omitempty"`

	// Phases is an optional operational overlay over the module DAG: named,
	// ordered groups that impose placement barriers and carry their own hooks.
	// The DAG remains the ordering authority — phases only add coarse barriers
	// (all of phase N placed before phase N+1) and a place to attach hooks.
	// When empty, all modules place in one implicit phase (today's behavior).
	// A dependency edge may never point forward across a phase boundary.
	Phases []Phase `yaml:"phases,omitempty" json:"phases,omitempty"`

	// CycleBreak names module pairs whose edge is a deferred feedback edge in a
	// declared binding cycle (design §6). A cycle among modules is an error
	// unless every edge in it is covered here (or the cluster is placed as one
	// atomic SCC batch). Each entry is "from->to".
	CycleBreak []string `yaml:"cycleBreak,omitempty" json:"cycleBreak,omitempty"`

	// Ignore declares source paths never read from any dir/git/oci source —
	// build artifacts and other derived output the baseline should not carry
	// (e.g. dist, .next, .turbo, .wrangler, coverage). Declared in the
	// blueprint so orun's core names no ecosystem (invariant 8). Each entry is
	// matched against every path segment (a bare name like ".next") and against
	// the whole relative path via path.Match (a glob like "**/dist"). Paths so
	// matched are excluded from both the source digest and placement.
	Ignore []string `yaml:"ignore,omitempty" json:"ignore,omitempty"`
}

// BlueprintMetadata carries identity for the blueprint.
type BlueprintMetadata struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// InputType enumerates the closed set of input field types (design §7, SC7).
type InputType string

const (
	InputString  InputType = "string"
	InputNumber  InputType = "number"
	InputBoolean InputType = "boolean"
	InputEnum    InputType = "enum"
	InputObject  InputType = "object"
	InputArray   InputType = "array"
)

// InputSpec is one typed field of a blueprint's inputs schema.
type InputSpec struct {
	Type     InputType `yaml:"type" json:"type"`
	Required bool      `yaml:"required,omitempty" json:"required,omitempty"`
	Default  any       `yaml:"default,omitempty" json:"default,omitempty"`
	// Values is the closed set for enum fields.
	Values []string `yaml:"values,omitempty" json:"values,omitempty"`
	// Pattern is an RE2 regexp a string field must match.
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	// Secret marks a field collected without echo and held in memory only; it
	// MUST NOT be written into any generated file (design §8).
	Secret      bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// SourceKind enumerates the closed set of source resolvers (design §5).
type SourceKind string

const (
	SourceInline SourceKind = "inline"
	SourceDir    SourceKind = "dir"
	SourceOCI    SourceKind = "oci"
	SourceGit    SourceKind = "git"
)

// SourceSpec declares one place module content is fetched from. Every resolved
// source is pinned by digest into the content-addressed object store before
// any module reads it (design §5).
type SourceSpec struct {
	Name string     `yaml:"name" json:"name"`
	Kind SourceKind `yaml:"kind" json:"kind"`
	// Path is the local path for kind: dir.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Repo/Ref locate a kind: git source (repo@ref → commit digest).
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`
	Ref  string `yaml:"ref,omitempty" json:"ref,omitempty"`
	// Ref (OCI) is a package reference for kind: oci.
	Package string `yaml:"package,omitempty" json:"package,omitempty"`
	// Digest, when set, pins the source explicitly (reproducible re-runs).
	Digest string `yaml:"digest,omitempty" json:"digest,omitempty"`
}

// PlacementMode is the closed set of the three placement modes (design §4).
type PlacementMode string

const (
	// ModeTemplate renders each file under from through text/template.
	ModeTemplate PlacementMode = "template"
	// ModeCopy copies verbatim bytes, no engine.
	ModeCopy PlacementMode = "copy"
	// ModeConsume records a pinned dependency and emits no bytes.
	ModeConsume PlacementMode = "consume"
)

// Module is the atom of scaffolding. One module = a component; many = a repo.
type Module struct {
	Name string        `yaml:"name" json:"name"`
	Mode PlacementMode `yaml:"mode" json:"mode"`
	// Source selects a sources[] entry by name; empty ⇒ the module carries an
	// inline Files body in the blueprint.
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
	// From is the path in the source (templated). To is the path in the target
	// (templated, path-contained).
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	To   string `yaml:"to,omitempty" json:"to,omitempty"`
	// Files carries inline file bodies when Source is empty. Keyed by target
	// path (relative, templated); each value is a template body.
	Files map[string]string `yaml:"files,omitempty" json:"files,omitempty"`
	// Bind names the files (relative to From/To) that legitimately interpolate
	// inputs. A template outside Bind that references .inputs is a lint error.
	Bind []string `yaml:"bind,omitempty" json:"bind,omitempty"`
	// DependsOn declares extra prerequisite edges for ordering (design §6).
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	// Wiring is an additional declared-edge source (treated like DependsOn).
	Wiring []string `yaml:"wiring,omitempty" json:"wiring,omitempty"`
}

// Hooks declares ecosystem post-steps (design §12).
type Hooks struct {
	PostInstantiate []Hook `yaml:"postInstantiate,omitempty" json:"postInstantiate,omitempty"`
}

// Hook is one declared post-step. It is exactly one of:
//   - Run: an explicit argv, no shell (the shipped form), or
//   - Workflow: a torkflow workflow file run through the workflow backend
//     (specs/orun-workflows §3, Surface B).
type Hook struct {
	ID  string   `yaml:"id" json:"id"`
	Run []string `yaml:"run,omitempty" json:"run,omitempty"`
	// Workflow names a torkflow workflow file (resolved against the blueprint's
	// directory) to run as this hook. Exactly one of Run/Workflow may be set.
	Workflow string `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	// With is the declared inputs handed to the workflow as its Trigger context.
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

// IsWorkflow reports whether this hook runs a workflow (vs. an argv).
func (h Hook) IsWorkflow() bool { return strings.TrimSpace(h.Workflow) != "" }

// validate enforces that a hook is exactly one of run / workflow (Surface B
// mutual exclusion, orun-workflows §3). Fail-closed.
func (h Hook) validate() error {
	hasRun := len(h.Run) > 0
	hasWorkflow := strings.TrimSpace(h.Workflow) != ""
	switch {
	case hasRun && hasWorkflow:
		return fmt.Errorf("hook %q sets both run and workflow — a hook must use exactly one", h.ID)
	case !hasRun && !hasWorkflow:
		return fmt.Errorf("hook %q sets neither run nor workflow", h.ID)
	}
	return nil
}

// Phase is one operational stage: an ordered group of modules placed as a
// barrier, with its own hooks run (in phase order) after placement. Approval
// gates + resumable pausing are a planned follow-on; this overlay adds the
// grouping, the barrier, and the hook attachment point.
type Phase struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Modules names the modules placed in this phase (by module name).
	Modules []string `yaml:"modules" json:"modules"`
	// Hooks run after this phase's modules are placed, before later phases'
	// hooks (opt-in via --run-hooks, outside the sandbox — design §12).
	Hooks []Hook `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

// ParseBlueprint decodes and structurally validates a Blueprint document.
func ParseBlueprint(data []byte) (*Blueprint, error) {
	var bp Blueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		return nil, fmt.Errorf("parse blueprint: %w", err)
	}
	if err := bp.validate(); err != nil {
		return nil, err
	}
	return &bp, nil
}

// validate enforces the envelope and closed-set invariants that are cheap to
// check before any inputs are collected or sources resolved.
func (bp *Blueprint) validate() error {
	if bp.APIVersion != BlueprintAPIVersion {
		return fmt.Errorf("blueprint apiVersion must be %q, got %q", BlueprintAPIVersion, bp.APIVersion)
	}
	if bp.Kind != BlueprintKind {
		return fmt.Errorf("blueprint kind must be %q, got %q", BlueprintKind, bp.Kind)
	}
	if bp.Metadata.Name == "" {
		return fmt.Errorf("blueprint metadata.name is required")
	}
	if len(bp.Modules) == 0 {
		return fmt.Errorf("blueprint must declare at least one module")
	}

	sourceNames := make(map[string]struct{}, len(bp.Sources))
	for i, s := range bp.Sources {
		if s.Name == "" {
			return fmt.Errorf("sources[%d]: name is required", i)
		}
		if _, dup := sourceNames[s.Name]; dup {
			return fmt.Errorf("sources[%d]: duplicate source name %q", i, s.Name)
		}
		sourceNames[s.Name] = struct{}{}
		switch s.Kind {
		case SourceInline, SourceDir, SourceOCI, SourceGit:
		default:
			return fmt.Errorf("sources[%d] (%s): unknown kind %q", i, s.Name, s.Kind)
		}
	}

	moduleNames := make(map[string]struct{}, len(bp.Modules))
	for i, m := range bp.Modules {
		if m.Name == "" {
			return fmt.Errorf("modules[%d]: name is required", i)
		}
		if _, dup := moduleNames[m.Name]; dup {
			return fmt.Errorf("modules[%d]: duplicate module name %q", i, m.Name)
		}
		moduleNames[m.Name] = struct{}{}
		switch m.Mode {
		case ModeTemplate, ModeCopy, ModeConsume:
		default:
			return fmt.Errorf("modules[%d] (%s): unknown mode %q", i, m.Name, m.Mode)
		}
		if m.Source != "" {
			if _, ok := sourceNames[m.Source]; !ok {
				return fmt.Errorf("modules[%d] (%s): references unknown source %q", i, m.Name, m.Source)
			}
		} else if len(m.Files) == 0 && m.Mode != ModeConsume {
			return fmt.Errorf("modules[%d] (%s): has no source and no inline files", i, m.Name)
		}
	}
	// Validate declared edges point at real modules.
	for i, m := range bp.Modules {
		for _, dep := range append(append([]string{}, m.DependsOn...), m.Wiring...) {
			if _, ok := moduleNames[dep]; !ok {
				return fmt.Errorf("modules[%d] (%s): dependsOn unknown module %q", i, m.Name, dep)
			}
		}
	}

	if err := bp.validatePhases(moduleNames); err != nil {
		return err
	}
	if err := bp.validateHooks(); err != nil {
		return err
	}
	return nil
}

// validateHooks enforces the run|workflow mutual-exclusion invariant across every
// declared hook — the global postInstantiate list and each phase's hooks.
func (bp *Blueprint) validateHooks() error {
	for i, h := range bp.Hooks.PostInstantiate {
		if err := h.validate(); err != nil {
			return fmt.Errorf("hooks.postInstantiate[%d]: %w", i, err)
		}
	}
	for pi, ph := range bp.Phases {
		for i, h := range ph.Hooks {
			if err := h.validate(); err != nil {
				return fmt.Errorf("phases[%d] (%s) hooks[%d]: %w", pi, ph.Name, i, err)
			}
		}
	}
	return nil
}

// validatePhases enforces the phase overlay invariants (when phases are
// declared): unique non-empty names, exact module coverage (every module in
// exactly one phase), and the barrier law — no dependency edge may point
// forward across a phase boundary (a module's prerequisites must be placed in
// its phase or earlier). Fail-closed.
func (bp *Blueprint) validatePhases(moduleNames map[string]struct{}) error {
	if len(bp.Phases) == 0 {
		return nil
	}
	phaseOf := make(map[string]int, len(moduleNames))
	seenPhase := make(map[string]struct{}, len(bp.Phases))
	for pi, ph := range bp.Phases {
		if ph.Name == "" {
			return fmt.Errorf("phases[%d]: name is required", pi)
		}
		if _, dup := seenPhase[ph.Name]; dup {
			return fmt.Errorf("phases[%d]: duplicate phase name %q", pi, ph.Name)
		}
		seenPhase[ph.Name] = struct{}{}
		for _, mod := range ph.Modules {
			if _, ok := moduleNames[mod]; !ok {
				return fmt.Errorf("phases[%d] (%s): references unknown module %q", pi, ph.Name, mod)
			}
			if prev, dup := phaseOf[mod]; dup {
				return fmt.Errorf("module %q is in two phases (%q and %q); each module belongs to exactly one phase",
					mod, bp.Phases[prev].Name, ph.Name)
			}
			phaseOf[mod] = pi
		}
	}
	// Every module must be covered.
	var uncovered []string
	for name := range moduleNames {
		if _, ok := phaseOf[name]; !ok {
			uncovered = append(uncovered, name)
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		return fmt.Errorf("phases declared but %d module(s) are in no phase: %s", len(uncovered), strings.Join(uncovered, ", "))
	}
	// Barrier law: for edge A→B (A depends on B), phase(B) <= phase(A).
	for _, m := range bp.Modules {
		for _, dep := range append(append([]string{}, m.DependsOn...), m.Wiring...) {
			if phaseOf[dep] > phaseOf[m.Name] {
				return fmt.Errorf("module %q (phase %q) depends on %q (phase %q) which is placed later — a dependency may not cross a phase barrier forward",
					m.Name, bp.Phases[phaseOf[m.Name]].Name, dep, bp.Phases[phaseOf[dep]].Name)
			}
		}
	}
	return nil
}
