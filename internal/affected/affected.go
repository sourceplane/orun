// Package affected is orun's single change-detection engine: one definition of
// "which components did this change touch?", consumed by plan --changed,
// run --changed, the cockpit, and orun catalog affected
// (specs/orun-catalog-state/change-detection.md).
//
// A Detector runs one surface-agnostic pipeline over a catalog read view
// (internal/objcatalog): a ChangeSource yields the changed file set (and any
// intent.yaml change), the ownership map classifies those files to components,
// the intent diff + intent-impact policy fold in intent-driven components, and
// the catalog dependency graph is walked for the forward (Dependencies) and
// reverse (Dependents) closures. The Result carries all three sets plus the
// Affected union every surface acts on.
//
// Identifiers are component keys throughout (the catalog's canonical id, shared
// by the ownership map and the dependency graph); surfaces map keys to names.
package affected

import (
	"context"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// IntentImpact is the policy applied to a global intent.yaml change.
type IntentImpact string

const (
	IntentImpactAll   IntentImpact = "all"   // every component is affected
	IntentImpactWatch IntentImpact = "watch" // only components watching a changed section
	IntentImpactNone  IntentImpact = "none"  // no component from a global change
)

// IntentMode classifies how (if at all) intent.yaml changed.
type IntentMode string

const (
	IntentModeNone       IntentMode = "none"       // unchanged or formatting-only
	IntentModeGlobal     IntentMode = "global"     // a catalog-wide section changed
	IntentModeComponents IntentMode = "components" // specific component blocks changed
)

// Confidence reports whether the selection is exact (high) or possibly stale
// (low — a structural change the loaded graph may not reflect).
type Confidence string

const (
	ConfidenceHigh Confidence = "high"
	ConfidenceLow  Confidence = "low"
)

// IntentChange carries whether intent.yaml is among the changed files and its
// before/after bytes for the semantic diff (empty when unavailable).
type IntentChange struct {
	Changed bool
	Base    []byte
	Head    []byte
}

// ChangeSource answers "which workspace files changed?" plus the intent signal.
// Two implementations feed the identical pipeline: GitChangeSource (a git diff)
// and the fingerprint source (the virtual Merkle tree — a later milestone).
type ChangeSource interface {
	ChangedPaths(ctx context.Context) (files []string, intent IntentChange, err error)
}

// ExplainEntry is one provenance record for --explain.
type ExplainEntry struct {
	Component string // component key ("" for a non-component note)
	Reason    string
}

// Result is the single shape every surface consumes.
type Result struct {
	DirectlyChanged  []string // components whose own inputs changed
	Dependencies     []string // forward deps of DirectlyChanged (full closure)
	Dependents       []string // transitive reverse deps of DirectlyChanged
	Affected         []string // the cockpit's blast radius (= DirectlyChanged ∪ Dependents)
	Selection        []string // the plan/run job set (= DirectlyChanged ∪ include:always forward closure)
	IntentMode       IntentMode
	Confidence       Confidence
	NeedsFullResolve bool // structural/global uncertainty (CD-2)
	Explain          []ExplainEntry
}

// Detector computes the affected set for a change, over one catalog read view.
type Detector struct {
	catalog *objcatalog.CatalogView
	policy  IntentImpact
}

// NewDetector builds a Detector. An empty/unknown policy defaults to "watch"
// (the historical default).
func NewDetector(catalog *objcatalog.CatalogView, policy IntentImpact) *Detector {
	switch policy {
	case IntentImpactAll, IntentImpactWatch, IntentImpactNone:
	default:
		policy = IntentImpactWatch
	}
	return &Detector{catalog: catalog, policy: policy}
}

// Detect runs the pipeline (change-detection.md §2.2) and returns the Result.
func (d *Detector) Detect(ctx context.Context, src ChangeSource) (Result, error) {
	files, intent, err := src.ChangedPaths(ctx)
	if err != nil {
		return Result{}, err
	}

	idx := d.index()
	directly := map[string]bool{}
	var explain []ExplainEntry
	res := Result{IntentMode: IntentModeNone, Confidence: ConfidenceHigh}

	// 1. Map changed files → components via the ownership map; flag structural.
	for _, f := range files {
		switch class := idx.classify(f); class.kind {
		case classComponent:
			if !directly[class.componentKey] {
				directly[class.componentKey] = true
				explain = append(explain, ExplainEntry{Component: class.componentKey, Reason: "input file changed: " + f})
			}
		case classStructural:
			// A component.yaml add/remove/edit may reshape the graph the loaded
			// snapshot can't reflect (CD-2): low confidence + full resolve, and
			// (best-effort) mark the owning component changed if we can map it.
			res.Confidence = ConfidenceLow
			res.NeedsFullResolve = true
			explain = append(explain, ExplainEntry{Reason: "structural change: " + f})
			if key := idx.ownerOf(f); key != "" && !directly[key] {
				directly[key] = true
				explain = append(explain, ExplainEntry{Component: key, Reason: "structural change in component dir: " + f})
			}
		case classGlobal:
			// handled by the intent stage below
		case classIgnore:
		}
	}

	// 2. Intent stage: classify the intent.yaml change and fold in components.
	if intent.Changed {
		mode, intentKeys, needsFull := d.applyIntent(intent, idx, &explain)
		res.IntentMode = mode
		if needsFull {
			res.Confidence = ConfidenceLow
			res.NeedsFullResolve = true
		}
		for _, key := range intentKeys {
			if !directly[key] {
				directly[key] = true
			}
		}
	}

	// 2b. Build-input rescope: a component whose declared build inputs
	// (dependsOn entries marked input:true) changed is itself directly
	// changed — its artifact embeds the dependency's sources, so "only
	// packages/x moved" still means this component's next build differs.
	// Transitive over input edges (contracts → sdk → console). This is the
	// systemic fix for the shared-package gap where a change under a
	// dependency's directory never scheduled the consumer's deploy.
	for _, key := range idx.inputDependentsClosure(directly) {
		if !directly[key] {
			directly[key] = true
			explain = append(explain, ExplainEntry{Component: key, Reason: "build input changed (dependsOn input:true)"})
		}
	}

	res.DirectlyChanged = sortedKeys(directly)

	// 3. Dependency closure over the catalog graph.
	res.Dependencies = idx.forwardClosure(directly)
	res.Dependents = idx.reverseClosure(directly)

	// 4. Affected = DirectlyChanged ∪ Dependents (the cockpit's blast radius, §6).
	affected := map[string]bool{}
	for k := range directly {
		affected[k] = true
	}
	for _, k := range res.Dependents {
		affected[k] = true
	}
	res.Affected = sortedKeys(affected)

	// 5. Selection = DirectlyChanged ∪ include:always forward closure — the job
	// set plan/run act on (parity with the existing --changed plan, which pulls
	// in include:always dependencies of the changed components).
	selection := map[string]bool{}
	for k := range directly {
		selection[k] = true
	}
	for _, k := range idx.includeAlwaysClosure(directly) {
		selection[k] = true
	}
	res.Selection = sortedKeys(selection)

	res.Explain = explain
	return res, nil
}

// applyIntent runs DiffIntent and turns the result into affected component keys
// per the intent-impact policy. Returns the mode, the keys, and whether the
// change warrants a full resolve (a structural global, CD-2).
func (d *Detector) applyIntent(intent IntentChange, idx *catalogIndex, explain *[]ExplainEntry) (IntentMode, []string, bool) {
	// Undiffable change (base/head bytes unavailable — e.g. --files, or a missing
	// ref) is conservatively global (CD-1 over-report), matching the legacy path.
	var diff git.IntentDiffResult
	if intent.Base == nil || intent.Head == nil {
		diff = git.IntentDiffResult{Mode: git.IntentDiffGlobal, Reason: "intent base/head unavailable"}
	} else {
		diff = git.DiffIntent(intent.Base, intent.Head)
	}
	switch diff.Mode {
	case git.IntentDiffGlobal:
		*explain = append(*explain, ExplainEntry{Reason: "intent.yaml global change: " + strings.Join(diff.ChangedSections, ",")})
		switch d.policy {
		case IntentImpactAll:
			return IntentModeGlobal, idx.allKeys(), true
		case IntentImpactNone:
			return IntentModeGlobal, nil, true
		default: // watch
			var keys []string
			for _, c := range idx.components {
				if watchesIntersect(c.watches, diff.ChangedSections) {
					keys = append(keys, c.key)
					*explain = append(*explain, ExplainEntry{Component: c.key, Reason: "watches a changed intent section"})
				}
			}
			return IntentModeGlobal, keys, true
		}
	case git.IntentDiffComponents:
		var keys []string
		for _, name := range append(append(append([]string{}, diff.Added...), diff.Modified...), diff.Removed...) {
			if key := idx.keyForName(name); key != "" {
				keys = append(keys, key)
				*explain = append(*explain, ExplainEntry{Component: key, Reason: "intent.yaml component block changed"})
			}
		}
		return IntentModeComponents, keys, false
	default: // none — formatting/comment-only
		return IntentModeNone, nil, false
	}
}

// watchesIntersect reports whether any watched section is among the changed
// sections (preserved verbatim from the legacy path, CD-3).
func watchesIntersect(watches, sections []string) bool {
	for _, w := range watches {
		for _, s := range sections {
			if w == s {
				return true
			}
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
