package provenance

import (
	"fmt"
	"sort"
	"strings"
)

// standards.go — the provenance rules under a MODE, plus the rules that need
// more than the webhook can see (orun-cloud saas-agent-skills SK5, design
// §7). The Go twin of packages/db/src/provenance/standards.ts; the two
// replay fixtures/standards-conformance.json byte-identically, the way
// Verify and the cloud evaluator replay provenance-conformance.json.
//
// Verify (provenance.go) is the IS7 v1 engine and does not change. This
// wraps it:
//
//	off      → nothing is said: `orun pr check --standards off` is today's
//	           command with the preflight switched off.
//	warn     → the v1 findings as they are, plus the new rules as warnings.
//	           The default. Never fails a PR that passes today.
//	enforce  → the v1 errors as they are; the v1 WARNINGS become errors (a
//	           missing manifest, missing skill pins); the new rules fail
//	           where design §7 says they fail.
//
// Every rule here is a line in the `orunbase` skill's non-negotiables, and
// each finding names the section to read, so a refusal is also the
// instruction. The new rules take FACTS, not access: the caller resolves
// what it can (the CLI in CI has the checkout and the registry; the cloud
// evaluator has the webhook) and leaves the rest nil, and a rule with no
// fact to judge says nothing.

// Mode is the standards mode.
type Mode string

const (
	ModeOff     Mode = "off"
	ModeWarn    Mode = "warn"
	ModeEnforce Mode = "enforce"
)

// DefaultMode is what applies when nothing names a mode.
const DefaultMode = ModeWarn

// Modes lists the accepted modes, for help text and errors.
var Modes = []Mode{ModeOff, ModeWarn, ModeEnforce}

// ParseMode reads a mode from a flag, an environment variable or intent.yaml;
// "" means unset (the caller falls through to the next source).
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return "", nil
	case ModeOff, ModeWarn, ModeEnforce:
		return m, nil
	default:
		return "", fmt.Errorf("standards mode %q is not one of off, warn, enforce", s)
	}
}

// The rules the standards check adds to Verify's five.
const (
	RuleSkillCurrent   = "skill-current"
	RuleTaskContract   = "task-contract"
	RuleAffectsCeiling = "affects-ceiling"
	RuleEpicStatus     = "epic-status"
	// RuleWorkManifest judges the declared work tree (saas-work-gitops
	// design §2, §5): epic.yaml well-formed, every listed key with a
	// contract, every reserved-prefix contract listed once.
	RuleWorkManifest = "work-manifest"
)

// SkillPinStatus is how a pinned skill revision stands against the registry.
type SkillPinStatus string

const (
	SkillCurrent SkillPinStatus = "current" // the registry's latest
	SkillStale   SkillPinStatus = "stale"   // a real revision, since superseded
	SkillUnknown SkillPinStatus = "unknown" // never published
)

// ContractFact is what the caller knows about tasks/<KEY>.TaskContract.yaml.
// Attached nil means "could not tell" and says nothing.
type ContractFact struct {
	Present  bool  `json:"present"`
	Attached *bool `json:"attached,omitempty"`
	// Path is the repo-relative file the caller looked for, as intent.yaml
	// `work.tasks` resolves it (saas-work-gitops design §1). "" prints the
	// default layout, so callers that predate the section keep their text.
	Path string `json:"path,omitempty"`
}

// EpicFact is what the caller knows about the milestone this PR closes.
type EpicFact struct {
	ClosesMilestone bool   `json:"closesMilestone"`
	TouchesStatus   bool   `json:"touchesStatus"`
	StatusPath      string `json:"statusPath,omitempty"`
}

// WorkFact is what the caller knows about the declared work tree.
type WorkFact struct {
	Problems []string `json:"problems"`
}

// StandardsInput is CheckInput plus the facts the new rules judge. A nil
// fact is "unknown", and its rule is silent.
type StandardsInput struct {
	CheckInput
	// SkillStatus resolves each manifest pin against the registry.
	SkillStatus map[string]SkillPinStatus
	// Contract: present in the tree, attached to the task.
	Contract *ContractFact
	// AffectsOutside lists the components the diff touched outside the
	// contract's affects (from the `task check --base` engine). nil = not
	// computed; an empty, non-nil slice = computed and clean.
	AffectsOutside []string
	// Epic: whether this PR closes a milestone and updates the epic's status.
	Epic *EpicFact
	// Work: the declared tree's problems, each a sentence naming the file
	// (workfile.Tree.Problems). nil = the repository declares no work
	// section, or the caller did not read the tree; an empty, non-nil slice
	// = read and clean.
	Work *WorkFact
}

const readTheSkill = "see the `orunbase` skill, §2 Non-negotiables"

// CheckStandards runs Verify under the mode and adds the new rules.
func CheckStandards(in StandardsInput, mode Mode) []Finding {
	if mode == "" {
		mode = DefaultMode
	}
	if mode == ModeOff {
		return nil
	}
	enforce := mode == ModeEnforce
	levelFor := func() string {
		if enforce {
			return "error"
		}
		return "warn"
	}

	out := make([]Finding, 0, 4)
	// The v1 engine, verbatim — then the mode decides what a warning is.
	for _, f := range Verify(in.CheckInput) {
		if enforce && f.Level == "warn" {
			out = append(out, Finding{Level: "error", Rule: f.Rule, Text: f.Text + " (enforced; " + readTheSkill + ")"})
		} else {
			out = append(out, f)
		}
	}
	// Under enforce, a manifest with no pins is an error even when the
	// session recorded none: the pins are how a reviewer re-reads the
	// playbook.
	if enforce && in.Manifest != nil && len(in.Manifest.Skills) == 0 && !in.HasSkillPins {
		out = append(out, Finding{Level: "error", Rule: "skill-pins",
			Text: "the manifest names no skill revisions — pull the skills before you start, and open the PR with `orun pr open` (" + readTheSkill + ")"})
	}

	// skill-current: unknown is an error under enforce; stale only warns (a
	// practice that changed while the PR was open is not the author's fault).
	if in.SkillStatus != nil {
		names := make([]string, 0, len(in.SkillStatus))
		for n := range in.SkillStatus {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			status := in.SkillStatus[name]
			if status == SkillCurrent {
				continue
			}
			level := "warn"
			if enforce && status == SkillUnknown {
				level = "error"
			}
			text := "skill " + name + " is pinned to a superseded revision — `orun skills pull` and re-read it"
			if status == SkillUnknown {
				text = "skill " + name + " is pinned to a revision the registry never published (" + readTheSkill + ")"
			}
			out = append(out, Finding{Level: level, Rule: RuleSkillCurrent, Text: text})
		}
	}

	// task-contract: the contract exists and is attached. The key resolves
	// the way the v1 rules resolve it: the caller's, else the branch's, else
	// the manifest's.
	if in.Contract != nil {
		key := in.TaskKey
		if key == "" {
			key = TaskKeyOfBranch(in.Branch)
		}
		if key == "" && in.Manifest != nil {
			key = in.Manifest.Task
		}
		if key == "" {
			key = "<KEY>"
		}
		path := in.Contract.Path
		if path == "" {
			path = "tasks/" + key + ".TaskContract.yaml"
		}
		switch {
		case !in.Contract.Present:
			out = append(out, Finding{Level: levelFor(), Rule: RuleTaskContract,
				Text: fmt.Sprintf("%s is missing — write it and `orun task attach %s` (%s)", path, key, readTheSkill)})
		case in.Contract.Attached != nil && !*in.Contract.Attached:
			out = append(out, Finding{Level: levelFor(), Rule: RuleTaskContract,
				Text: fmt.Sprintf("%s is not attached to %s — `orun task attach %s` (%s)", path, key, key, readTheSkill)})
		}
	}

	// affects-ceiling: the diff stays inside the contract. ASCII only in
	// the texts, like the v1 rules: the two engines' quoting agrees on
	// ASCII and the fixture keeps it that way.
	if len(in.AffectsOutside) > 0 {
		shown := in.AffectsOutside
		more := ""
		if len(shown) > 5 {
			shown = shown[:5]
			more = ", ..."
		}
		out = append(out, Finding{Level: levelFor(), Rule: RuleAffectsCeiling,
			Text: fmt.Sprintf("%d component(s) outside the contract's affects: %s%s — widen the contract or narrow the change (%s)",
				len(in.AffectsOutside), strings.Join(shown, ", "), more, readTheSkill)})
	}

	// work-manifest: one finding per problem, in the tree's order — a PR
	// fixes the whole list at once.
	if in.Work != nil {
		for _, p := range in.Work.Problems {
			out = append(out, Finding{Level: levelFor(), Rule: RuleWorkManifest, Text: p + " (" + readTheSkill + ")"})
		}
	}

	// epic-status: always a warning (design §7) — the status file is the
	// record, but a follow-up PR may carry it.
	if in.Epic != nil && in.Epic.ClosesMilestone && !in.Epic.TouchesStatus {
		path := in.Epic.StatusPath
		if path == "" {
			path = "the epic's IMPLEMENTATION-STATUS.md"
		}
		out = append(out, Finding{Level: "warn", Rule: RuleEpicStatus,
			Text: "this PR closes a milestone but does not update " + path})
	}
	return out
}
