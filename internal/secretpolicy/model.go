// Package secretpolicy is orun's compile-time loader and model for the portable
// Layer-2 SecretPolicy document (apiVersion orun.io/v1, kind SecretPolicy —
// specs/orun-secrets/policy-model.md, data-model.md §4).
//
// It parses and validates the locked predicate vocabulary, discovers documents
// across the three placement tiers (composition-attached → stack-wide → intent
// overlay), injects the constitutional component.type scope onto composition
// fragments, and enforces the narrow-only downward rule (policy-model.md §5)
// that an intent overlay may only tighten, never widen, a higher tier's grants.
//
// The fetch-time evaluator is a separate pure library in the config-worker; this
// package is the authoring/compile side. No secret value is ever involved here —
// policies are conditions, not values — but errors stay clean regardless.
package secretpolicy

import (
	"encoding/json"
	"fmt"
)

// Tier names a placement tier, in evaluation precedence order.
type Tier string

const (
	// TierComposition is a composition-attached fragment, force-scoped to its
	// own component.type (compositions/<type>/secret-policy.yaml).
	TierComposition Tier = "composition"
	// TierStack is a stack-wide document (<stackRoot>/policies/*.SecretPolicy.yaml).
	TierStack Tier = "stack"
	// TierIntent is a repo overlay (policies/*.SecretPolicy.yaml next to intent.yaml).
	TierIntent Tier = "intent"
)

// Effect is a rule's decision.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// PredicateKind enumerates the locked predicate vocabulary (SD-7,
// policy-model.md §6): equals, in, glob matches, bool, team-membership,
// platform. AND-of-predicates within a rule; OR via multiple rules.
type PredicateKind string

const (
	PredEquals   PredicateKind = "equals"
	PredIn       PredicateKind = "in"
	PredMatches  PredicateKind = "matches"
	PredBool     PredicateKind = "bool"
	PredTeam     PredicateKind = "team"
	PredPlatform PredicateKind = "platform"
)

// Scope targets a rule at {env, key} with globs (most-specific-wins).
type Scope struct {
	Env string
	Key string
}

// Predicate is one parsed entry of a rule's when[]. It keeps the canonical
// authored text so the document round-trips faithfully to the backend, plus a
// structured decomposition for local validation and the narrow-only check.
type Predicate struct {
	Kind PredicateKind
	// Fact is the dotted axis path for equals/in/matches/bool.
	Fact string
	// Value is the equals literal (or the single platform value).
	Value string
	// Values holds the in[] members (or the platform[] list).
	Values []string
	// Glob is the matches[] pattern.
	Glob string
	// BoolWant is the expected value for a bool predicate (false for !field).
	BoolWant bool
	// Team is the slug for a team-membership predicate.
	Team string
	// text is the canonical authored form, re-emitted on push.
	text string
}

// String renders the predicate's canonical authored form.
func (p Predicate) String() string { return p.text }

// Rule is one SecretPolicy rule.
type Rule struct {
	ID       string
	Effect   Effect
	Subjects []string
	Scope    Scope
	When     []Predicate
}

// Document is a parsed, tier-tagged SecretPolicy document.
type Document struct {
	Name   string
	Tier   Tier
	Source string
	// Path is the file it was loaded from (diagnostics only).
	Path  string
	Rules []Rule
}

// wireRule is the JSON shape pushed to the backend and stored as JSONB. It
// matches the config-worker's parseRule/parseStringPredicate contract: scope
// always carries both env and key, and when[] is the authored DSL string form.
type wireRule struct {
	ID       string   `json:"id"`
	Effect   string   `json:"effect"`
	Subjects []string `json:"subjects,omitempty"`
	Scope    struct {
		Env string `json:"env"`
		Key string `json:"key"`
	} `json:"scope"`
	When []string `json:"when,omitempty"`
}

// DocumentJSON renders the document's spec ({ "rules": [...] }) as the backend
// PUT body's `document` field. Content-stable: the same document always
// serializes identically (push is idempotent by content hash).
func (d Document) DocumentJSON() (json.RawMessage, error) {
	rules := make([]wireRule, 0, len(d.Rules))
	for _, r := range d.Rules {
		var wr wireRule
		wr.ID = r.ID
		wr.Effect = string(r.Effect)
		wr.Subjects = r.Subjects
		wr.Scope.Env = r.Scope.Env
		wr.Scope.Key = r.Scope.Key
		for _, p := range r.When {
			wr.When = append(wr.When, p.String())
		}
		rules = append(rules, wr)
	}
	body := struct {
		Rules []wireRule `json:"rules"`
	}{Rules: rules}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("secretpolicy: marshaling document %q: %w", d.Name, err)
	}
	return data, nil
}
