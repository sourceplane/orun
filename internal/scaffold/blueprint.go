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
//   - Run: an explicit argv, no shell (the ecosystem escape), or
//   - Uses: a typed orun action resolved in-process (orun-bootstrap-engine
//     BE-O1), parameterized by With and yielding addressable Outputs, or
//   - Workflow: an orun workflow file run through the in-process flow engine
//     (specs/orun-workflows §3, Surface B). Retired by BE-O8.
type Hook struct {
	ID  string   `yaml:"id" json:"id"`
	Run []string `yaml:"run,omitempty" json:"run,omitempty"`
	// Uses names a registered action — `<namespace>/<verb>@v<major>`. Its With
	// block is validated against the action's declared parameters at PARSE
	// time, so a misspelled parameter is a blueprint error naming the line
	// rather than an exit code partway through a bootstrap.
	Uses string `yaml:"uses,omitempty" json:"uses,omitempty"`
	// Narrate is one authored line, emitted when this hook completes (BE-O6).
	Narrate string `yaml:"narrate,omitempty" json:"narrate,omitempty"`
	// Workflow names an orun workflow file (resolved against the blueprint's
	// directory) to run as this hook. Exactly one of Run/Workflow may be set.
	Workflow string `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	// With is the declared inputs handed to the workflow as its Trigger context.
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
	// Connections is the credential grant for a workflow hook
	// (orun-workflows-v2 §4): workflow connection name → credential field →
	// blueprint input name (which MUST be declared secret: true). Validated
	// before placement against the connections the workflow file declares; only
	// mapped inputs are injected.
	Connections map[string]map[string]string `yaml:"connections,omitempty" json:"connections,omitempty"`
}

// IsWorkflow reports whether this hook runs a workflow (vs. an argv or action).
func (h Hook) IsWorkflow() bool { return strings.TrimSpace(h.Workflow) != "" }

// IsAction reports whether this hook calls a registered orun action.
func (h Hook) IsAction() bool { return strings.TrimSpace(h.Uses) != "" }

// validate enforces that a hook is exactly ONE of run / uses / workflow.
// Fail-closed: two is ambiguous and zero is a hook that does nothing, and both
// are far more likely to be an unfinished edit than an intention.
func (h Hook) validate() error {
	var set []string
	if len(h.Run) > 0 {
		set = append(set, "run")
	}
	if h.IsAction() {
		set = append(set, "uses")
	}
	if h.IsWorkflow() {
		set = append(set, "workflow")
	}
	switch len(set) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("hook %q sets none of run, uses or workflow", h.ID)
	default:
		return fmt.Errorf("hook %q sets %s — a hook must use exactly one", h.ID, strings.Join(set, " and "))
	}
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
	// Hooks run around this phase's placement (opt-in via --run-hooks, outside
	// the sandbox — design §12). A bare list is `post`, which is what a phase's
	// hooks have always meant.
	Hooks PhaseHooks `yaml:"hooks,omitempty" json:"hooks,omitempty"`

	// When is a CEL expression over `inputs`. A phase whose condition is false
	// is skipped — declared, rather than a caller remembering not to ask for
	// it (orun-bootstrap-engine BE-O3). Empty means always.
	When string `yaml:"when,omitempty" json:"when,omitempty"`
	// Requires states what must already be true before this phase may run.
	Requires *PhaseRequires `yaml:"requires,omitempty" json:"requires,omitempty"`
	// Retry governs this phase's HOOKS, not its placement. Placement is
	// deterministic and retrying it changes nothing; hooks reach the network
	// and are where a transient failure actually lives.
	Retry *RetrySpec `yaml:"retry,omitempty" json:"retry,omitempty"`

	// Title is what a person calls this phase. Empty falls back to Name.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// ExpectedMinutes is a measured budget, shown so an operator knows whether
	// to wait. Zero means unstated.
	ExpectedMinutes int `yaml:"expectedMinutes,omitempty" json:"expectedMinutes,omitempty"`
	// Narrate is the prose THIS BASELINE authored for the phase's transitions
	// (orun-bootstrap-engine BE-O6). Reviewed in a pull request, diffed like
	// code, identical on every run — which is the whole difference from a model
	// paraphrasing a transcript.
	Narrate *Narration `yaml:"narrate,omitempty" json:"narrate,omitempty"`
}

// Narration is a phase's authored lines, one per transition.
type Narration struct {
	Start  string `yaml:"start,omitempty" json:"start,omitempty"`
	Await  string `yaml:"await,omitempty" json:"await,omitempty"`
	Done   string `yaml:"done,omitempty" json:"done,omitempty"`
	Failed string `yaml:"failed,omitempty" json:"failed,omitempty"`
}

// Line returns the authored line for a state, or empty.
func (n *Narration) Line(state EventState) string {
	if n == nil {
		return ""
	}
	switch state {
	case EventStarted:
		return n.Start
	case EventWaiting:
		return n.Await
	case EventDone:
		return n.Done
	case EventFailed:
		return n.Failed
	}
	return ""
}

// PhaseHooks are a phase's three hook slots (orun-bootstrap-engine BE-O4).
//
//	pre    before placement — create the work item, announce the phase
//	post   after placement  — the ecosystem escape, the landing
//	await  after post       — the WAIT: an action here may report `pending`,
//	                          which parks the phase instead of failing it
//
// Authored either as a bare list, which means `post` and is what a phase's
// hooks have always meant, or as a mapping naming the slots. Both forms parse,
// so no existing blueprint changes and a phase that needs a wait says so.
type PhaseHooks struct {
	Pre   []Hook `yaml:"pre,omitempty" json:"pre,omitempty"`
	Post  []Hook `yaml:"post,omitempty" json:"post,omitempty"`
	Await []Hook `yaml:"await,omitempty" json:"await,omitempty"`
}

// UnmarshalYAML accepts the legacy list form and the slot mapping.
func (h *PhaseHooks) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		return value.Decode(&h.Post)
	case yaml.MappingNode:
		// A named type, so decoding the mapping does not recurse into this
		// method forever.
		type slots PhaseHooks
		var s slots
		if err := value.Decode(&s); err != nil {
			return err
		}
		*h = PhaseHooks(s)
		return nil
	default:
		return fmt.Errorf("phase hooks must be a list or a mapping of pre/post/await, got %v", value.Kind)
	}
}

// All returns every hook in run order.
func (h PhaseHooks) All() []Hook {
	out := make([]Hook, 0, len(h.Pre)+len(h.Post)+len(h.Await))
	out = append(out, h.Pre...)
	out = append(out, h.Post...)
	out = append(out, h.Await...)
	return out
}

// Len is the total hook count.
func (h PhaseHooks) Len() int { return len(h.Pre) + len(h.Post) + len(h.Await) }

// PhaseRequires is a phase's precondition, in two halves that answer different
// questions (orun-bootstrap-engine BE-O3).
//
// Phases is a PLACEMENT check, answered by deriving the tree — never by
// reading a record that says a phase ran.
//
// Probe is a REALITY check, answered by running actions. It exists because
// placement cannot know whether what an earlier phase deployed is still there.
// A baseline's own docs carry this knowledge as prose today — "this lane fails
// because phase 03 is incomplete; re-run phase 03" — and prose cannot gate
// anything.
type PhaseRequires struct {
	Phases []string `yaml:"phases,omitempty" json:"phases,omitempty"`
	Probe  []Hook   `yaml:"probe,omitempty" json:"probe,omitempty"`
}

// RetrySpec bounds retries of a phase's hooks.
type RetrySpec struct {
	Attempts int `yaml:"attempts" json:"attempts"`
	// BackoffSeconds is the base delay; the wait is attempt × backoff, so a
	// phase that fails three times waits 1×, then 2×, then 3×.
	BackoffSeconds int `yaml:"backoffSeconds,omitempty" json:"backoffSeconds,omitempty"`
}

// ParseBlueprint decodes and structurally validates a Blueprint document.
//
// Action hooks are checked here rather than at placement time (BE-O1): a
// misspelled parameter must be a parse error naming the line, not a failure
// discovered partway through writing a customer's repository.
func ParseBlueprint(data []byte) (*Blueprint, error) {
	var bp Blueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		return nil, fmt.Errorf("parse blueprint: %w", err)
	}
	if err := bp.validate(); err != nil {
		return nil, err
	}
	if err := validateActionHooks(&bp, newHookLocator(data)); err != nil {
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
		for i, h := range ph.Hooks.All() {
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
	return bp.validatePhaseGates()
}

// validatePhaseGates checks `when`, `requires` and `retry` (BE-O3). Every one
// of these fails at parse time or not at all: a phase that silently never runs
// because its condition does not compile is the worst outcome, since nothing
// appears to be wrong.
func (bp *Blueprint) validatePhaseGates() error {
	index := make(map[string]int, len(bp.Phases))
	for i, ph := range bp.Phases {
		index[ph.Name] = i
	}
	for pi, ph := range bp.Phases {
		where := fmt.Sprintf("phases[%d] (%s)", pi, ph.Name)
		if err := compileCondition(ph.When, where+" when"); err != nil {
			return err
		}
		if err := validateNarration(where, ph.Narrate); err != nil {
			return err
		}
		for hi, h := range ph.Hooks.All() {
			if err := checkNarrationLine(fmt.Sprintf("%s hooks[%d] (%s) narrate", where, hi, h.ID), h.Narrate); err != nil {
				return err
			}
		}
		if ph.Retry != nil {
			if ph.Retry.Attempts < 1 {
				return fmt.Errorf("%s retry.attempts must be at least 1, got %d", where, ph.Retry.Attempts)
			}
			if ph.Retry.BackoffSeconds < 0 {
				return fmt.Errorf("%s retry.backoffSeconds may not be negative", where)
			}
		}
		if ph.Requires == nil {
			continue
		}
		for _, need := range ph.Requires.Phases {
			at, ok := index[need]
			if !ok {
				return fmt.Errorf("%s requires unknown phase %q", where, need)
			}
			// A phase may only require one placed BEFORE it. Requiring a later
			// phase is unsatisfiable by construction, and requiring itself is a
			// loop — both are edits nobody meant to make.
			if at >= pi {
				return fmt.Errorf("%s requires phase %q, which is not placed earlier — a requirement may only point backwards", where, need)
			}
		}
		for hi, h := range ph.Requires.Probe {
			if !h.IsAction() {
				return fmt.Errorf("%s requires.probe[%d] (%s): a probe must be an action (`uses:`) — it answers a question, it does not run a command", where, hi, h.ID)
			}
			if err := h.validate(); err != nil {
				return fmt.Errorf("%s requires.probe[%d]: %w", where, hi, err)
			}
		}
	}
	return nil
}
