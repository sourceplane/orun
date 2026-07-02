package secretpolicy

import (
	"fmt"
	"strings"
)

// Severity ranks a Finding.
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Finding is one lint diagnostic — a vocabulary or narrow-only violation
// reported without necessarily aborting.
type Finding struct {
	Severity Severity
	Tier     Tier
	Source   string
	Path     string
	RuleID   string
	Kind     string // "vocabulary" | "narrow-only" | "io"
	Message  string
}

// Lint reports narrow-only overlay violations (policy-model.md §5) across the
// tier-ordered documents without failing. An intent-tier allow whose grant is
// broader than every stack/composition allow is an error; intent deny rules are
// always accepted. Composition fragments are structurally type-scoped at load
// and are not re-checked here.
func Lint(tiers Tiers) []Finding {
	var findings []Finding
	higher := higherTierAllows(tiers)
	for _, doc := range tiers.Intent {
		for _, rule := range doc.Rules {
			if rule.Effect != EffectAllow {
				continue // intent deny is always accepted
			}
			if !coveredByHigher(rule, higher) {
				findings = append(findings, Finding{
					Severity: SevError,
					Tier:     TierIntent,
					Source:   doc.Source,
					Path:     doc.Path,
					RuleID:   rule.ID,
					Kind:     "narrow-only",
					Message: fmt.Sprintf(
						"intent allow %q is broader than any stack/composition allow (env=%q key=%q); an overlay may only tighten a higher tier's grant",
						rule.ID, rule.Scope.Env, rule.Scope.Key),
				})
			}
		}
	}
	return findings
}

// Validate is the strict counterpart the loader/push path uses: it returns a
// non-nil error aggregating every narrow-only violation.
func Validate(tiers Tiers) error {
	findings := Lint(tiers)
	if len(findings) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(findings))
	for _, f := range findings {
		msgs = append(msgs, f.Message)
	}
	return fmt.Errorf("narrow-only violation(s):\n  - %s", strings.Join(msgs, "\n  - "))
}

// higherTierAllows gathers every composition- and stack-tier allow rule — the
// ceiling an intent overlay must stay within.
func higherTierAllows(tiers Tiers) []Rule {
	var out []Rule
	for _, doc := range tiers.Composition {
		for _, r := range doc.Rules {
			if r.Effect == EffectAllow {
				out = append(out, r)
			}
		}
	}
	for _, doc := range tiers.Stack {
		for _, r := range doc.Rules {
			if r.Effect == EffectAllow {
				out = append(out, r)
			}
		}
	}
	return out
}

// coveredByHigher reports whether some higher-tier allow's grant-set is a
// superset of the intent rule's grant-set (conservative per-rule check).
func coveredByHigher(rule Rule, higher []Rule) bool {
	for _, h := range higher {
		if covers(h, rule) {
			return true
		}
	}
	return false
}

// covers reports whether higher-tier allow h grants at least everything narrow
// grants: h's scope globs cover narrow's, h trusts every subject narrow trusts,
// and narrow carries at least h's conditions (adding predicates only tightens).
// This is the conservative superset the narrow-only rule requires.
func covers(h, narrow Rule) bool {
	if !globCovers(h.Scope.Env, narrow.Scope.Env) {
		return false
	}
	if !globCovers(h.Scope.Key, narrow.Scope.Key) {
		return false
	}
	if !subjectsCovered(narrow.Subjects, h.Subjects) {
		return false
	}
	return conditionsCovered(narrow.When, h.When)
}

// globCovers reports whether every string the narrow glob matches is also
// matched by the broad glob (broad ⊇ narrow). Conservative: only `*`, exact
// literals, and single leading/trailing `*` frames are reasoned about precisely;
// anything more exotic that is not identical is treated as not-covering.
func globCovers(broad, narrow string) bool {
	if broad == "*" || broad == narrow {
		return true
	}
	if !strings.Contains(broad, "*") {
		// broad is an exact literal: it covers only itself (handled above).
		return false
	}
	if !strings.Contains(narrow, "*") {
		return globMatch(broad, narrow)
	}
	return globFrameCovers(broad, narrow)
}

// globFrameCovers handles the both-are-globs case for single-`*` broad frames.
func globFrameCovers(broad, narrow string) bool {
	if strings.Count(broad, "*") != 1 {
		return false
	}
	switch {
	case strings.HasSuffix(broad, "*") && !strings.HasPrefix(broad, "*"):
		prefix := strings.TrimSuffix(broad, "*")
		head := narrow
		if i := strings.IndexByte(narrow, '*'); i >= 0 {
			head = narrow[:i]
		}
		return strings.HasPrefix(head, prefix)
	case strings.HasPrefix(broad, "*") && !strings.HasSuffix(broad, "*"):
		suffix := strings.TrimPrefix(broad, "*")
		tail := narrow
		if i := strings.LastIndexByte(narrow, '*'); i >= 0 {
			tail = narrow[i+1:]
		}
		return strings.HasSuffix(tail, suffix)
	default:
		return false
	}
}

// globMatch matches a `*`-glob (the only wildcard the vocabulary allows)
// against a concrete literal.
func globMatch(glob, value string) bool {
	if glob == "*" {
		return true
	}
	if !strings.Contains(glob, "*") {
		return glob == value
	}
	parts := strings.Split(glob, "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false // leading anchor
		}
		pos += idx + len(part)
	}
	if !strings.HasSuffix(glob, "*") {
		return strings.HasSuffix(value, parts[len(parts)-1])
	}
	return true
}

// subjectsCovered reports whether every subject narrow trusts is also trusted
// by broad (narrow ⊆ broad). Empty means "any subject": a narrow "any" is
// covered only by a broad "any".
func subjectsCovered(narrow, broad []string) bool {
	broadAny := len(broad) == 0 || containsSubject(broad, "*authenticated")
	if len(narrow) == 0 {
		return broadAny
	}
	if broadAny {
		return true
	}
	for _, s := range narrow {
		if !subjectCoveredBy(s, broad) {
			return false
		}
	}
	return true
}

func subjectCoveredBy(s string, broad []string) bool {
	for _, b := range broad {
		if b == "*authenticated" || b == s {
			return true
		}
		// An actor-kind literal covers a specific principal of that kind.
		if b == "user" && strings.HasPrefix(s, "user:") {
			return true
		}
		if b == "service_principal" && strings.HasPrefix(s, "service_principal:") {
			return true
		}
	}
	return false
}

func containsSubject(subjects []string, want string) bool {
	for _, s := range subjects {
		if s == want {
			return true
		}
	}
	return false
}

// conditionsCovered reports whether narrow carries at least all of broad's
// predicates (narrow.When ⊇ broad.When by canonical text). More predicates on
// narrow only tightens; a missing broad predicate means narrow could match
// facts broad excludes, so it is not covered.
func conditionsCovered(narrow, broad []Predicate) bool {
	have := make(map[string]bool, len(narrow))
	for _, p := range narrow {
		have[p.String()] = true
	}
	for _, p := range broad {
		if !have[p.String()] {
			return false
		}
	}
	return true
}
