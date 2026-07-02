package secretpolicy

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	wantAPIVersion = "orun.io/v1"
	wantKind       = "SecretPolicy"
)

// stringFacts is the locked set of string-valued axes addressable in equals /
// in / matches predicates (policy-model.md §4; mirrors the config-worker
// evaluator's factValue). component.labels.<x> is handled separately.
var stringFacts = map[string]bool{
	"env":                true,
	"servesFrom":         true,
	"subject.id":         true,
	"subject.kind":       true,
	"component.type":     true,
	"component.domain":   true,
	"component.name":     true,
	"trigger.event":      true,
	"trigger.action":     true,
	"trigger.branch":     true,
	"trigger.baseBranch": true,
	"trigger.tag":        true,
	"trigger.actor":      true,
	"trigger.repository": true,
}

// boolFacts is the locked set of boolean axes addressable as a bare bool
// predicate (`trigger.declared` / `!trigger.declared`).
var boolFacts = map[string]bool{
	"trigger.declared": true,
}

// platformValues is the locked set of execution-platform values (§4.4).
var platformValues = map[string]bool{
	"local-cli": true,
	"ci-oidc":   true,
	"service":   true,
}

// subjectFormRE recognizes the prefixed subject forms (user:/team:/
// service_principal:) as well as the bare actor-kind literals and *authenticated.
var (
	kindLiterals    = map[string]bool{"workflow": true, "user": true, "service_principal": true}
	stringLiteralRE = regexp.MustCompile(`^"(.*)"$|^'(.*)'$`)
)

// rawDocument is the on-disk YAML shape.
type rawDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Rules []rawRule `yaml:"rules"`
	} `yaml:"spec"`
}

type rawRule struct {
	ID       string   `yaml:"id"`
	Effect   string   `yaml:"effect"`
	Subjects []string `yaml:"subjects"`
	Scope    struct {
		Env string `yaml:"env"`
		Key string `yaml:"key"`
	} `yaml:"scope"`
	When []string `yaml:"when"`
}

// ParseDocument parses and strictly validates a SecretPolicy YAML body. It
// rejects unknown effects, unknown predicate/field vocabulary, and malformed
// subjects with a clear error. Rules missing an id get a stable synthesized one
// (<name>-rule-<index>). Callers that need composition type-injection call
// (*Document).InjectComponentType afterwards.
func ParseDocument(data []byte, tier Tier, source, path string) (*Document, error) {
	var raw rawDocument
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", path, err)
	}
	if raw.APIVersion != wantAPIVersion {
		return nil, fmt.Errorf("%s: apiVersion must be %q, got %q", path, wantAPIVersion, raw.APIVersion)
	}
	if raw.Kind != wantKind {
		return nil, fmt.Errorf("%s: kind must be %q, got %q", path, wantKind, raw.Kind)
	}
	name := strings.TrimSpace(raw.Metadata.Name)
	if name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}

	doc := &Document{Name: name, Tier: tier, Source: source, Path: path}
	seen := map[string]bool{}
	for i, rr := range raw.Spec.Rules {
		rule, err := parseRule(rr, name, i)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[rule.ID] {
			return nil, fmt.Errorf("%s: duplicate rule id %q", path, rule.ID)
		}
		seen[rule.ID] = true
		doc.Rules = append(doc.Rules, rule)
	}
	return doc, nil
}

func parseRule(rr rawRule, docName string, index int) (Rule, error) {
	id := strings.TrimSpace(rr.ID)
	if id == "" {
		id = fmt.Sprintf("%s-rule-%d", docName, index)
	}
	effect := Effect(strings.TrimSpace(rr.Effect))
	if effect != EffectAllow && effect != EffectDeny {
		return Rule{}, fmt.Errorf("rule %q: effect must be %q or %q, got %q", id, EffectAllow, EffectDeny, rr.Effect)
	}
	for _, s := range rr.Subjects {
		if err := validateSubject(s); err != nil {
			return Rule{}, fmt.Errorf("rule %q: %w", id, err)
		}
	}
	scope := Scope{Env: rr.Scope.Env, Key: rr.Scope.Key}
	if scope.Env == "" {
		scope.Env = "*"
	}
	if scope.Key == "" {
		scope.Key = "*"
	}
	rule := Rule{ID: id, Effect: effect, Subjects: rr.Subjects, Scope: scope}
	for _, w := range rr.When {
		pred, err := parsePredicate(w)
		if err != nil {
			return Rule{}, fmt.Errorf("rule %q: %w", id, err)
		}
		rule.When = append(rule.When, pred)
	}
	return rule, nil
}

// validateSubject rejects a subject outside the portable subject vocabulary
// (policy-model.md §2).
func validateSubject(s string) error {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return fmt.Errorf("empty subject")
	}
	switch {
	case trimmed == "*authenticated":
		return nil
	case kindLiterals[trimmed]:
		return nil
	case strings.HasPrefix(trimmed, "user:"),
		strings.HasPrefix(trimmed, "team:"),
		strings.HasPrefix(trimmed, "service_principal:"):
		if strings.TrimSpace(trimmed[strings.IndexByte(trimmed, ':')+1:]) == "" {
			return fmt.Errorf("subject %q is missing its identifier", s)
		}
		return nil
	default:
		return fmt.Errorf("unknown subject %q (want user:<id>, team:<slug>, service_principal:<id>, workflow, user, service_principal, or *authenticated)", s)
	}
}

// parsePredicate parses one when[] string from the locked DSL, rejecting any
// shape or field outside the vocabulary. The canonical authored text is kept
// on the predicate for faithful re-emission on push.
func parsePredicate(expr string) (Predicate, error) {
	s := strings.TrimSpace(expr)
	if s == "" {
		return Predicate{}, fmt.Errorf("empty predicate")
	}

	// subject in team "<slug>"  |  subject in team:<slug>
	if m := regexp.MustCompile(`^subject\s+in\s+team[:\s]\s*(.+)$`).FindStringSubmatch(s); m != nil {
		team := unquote(strings.TrimSpace(m[1]))
		if team == "" {
			return Predicate{}, fmt.Errorf("predicate %q: team slug is empty", expr)
		}
		return Predicate{Kind: PredTeam, Team: team, text: fmt.Sprintf("subject in team %q", team)}, nil
	}

	// <fact> in [<v>, ...]
	if m := regexp.MustCompile(`^([\w.]+)\s+in\s+\[(.*)\]$`).FindStringSubmatch(s); m != nil {
		fact := m[1]
		values, err := parseList(m[2])
		if err != nil {
			return Predicate{}, fmt.Errorf("predicate %q: %w", expr, err)
		}
		if fact == "platform" {
			for _, v := range values {
				if !platformValues[v] {
					return Predicate{}, fmt.Errorf("predicate %q: unknown platform %q (want local-cli, ci-oidc, service)", expr, v)
				}
			}
			return Predicate{Kind: PredPlatform, Values: values, text: s}, nil
		}
		if err := requireStringFact(fact, expr); err != nil {
			return Predicate{}, err
		}
		return Predicate{Kind: PredIn, Fact: fact, Values: values, text: s}, nil
	}

	// <fact> matches "<glob>"
	if m := regexp.MustCompile(`^([\w.]+)\s+matches\s+(.+)$`).FindStringSubmatch(s); m != nil {
		fact := m[1]
		glob := unquote(strings.TrimSpace(m[2]))
		if glob == "" {
			return Predicate{}, fmt.Errorf("predicate %q: matches glob is empty", expr)
		}
		if err := requireStringFact(fact, expr); err != nil {
			return Predicate{}, err
		}
		return Predicate{Kind: PredMatches, Fact: fact, Glob: glob, text: fmt.Sprintf("%s matches %q", fact, glob)}, nil
	}

	// <fact> == <literal>
	if m := regexp.MustCompile(`^([\w.]+)\s*==\s*(.+)$`).FindStringSubmatch(s); m != nil {
		fact := m[1]
		lit := strings.TrimSpace(m[2])
		if fact == "platform" {
			v := unquote(lit)
			if !platformValues[v] {
				return Predicate{}, fmt.Errorf("predicate %q: unknown platform %q (want local-cli, ci-oidc, service)", expr, v)
			}
			return Predicate{Kind: PredPlatform, Value: v, Values: []string{v}, text: s}, nil
		}
		value, ok := coerceLiteral(lit)
		if !ok {
			return Predicate{}, fmt.Errorf("predicate %q: right-hand side must be a quoted string, number, or bool", expr)
		}
		if !stringFacts[fact] && !boolFacts[fact] && !isLabelFact(fact) {
			return Predicate{}, unknownFactErr(fact, expr)
		}
		return Predicate{Kind: PredEquals, Fact: fact, Value: value, text: s}, nil
	}

	// bare / negated boolean fact, e.g. `trigger.declared` or `!trigger.declared`
	want := true
	fact := s
	if strings.HasPrefix(s, "!") {
		want = false
		fact = strings.TrimSpace(s[1:])
	}
	if regexp.MustCompile(`^[\w.]+$`).MatchString(fact) {
		if !boolFacts[fact] {
			return Predicate{}, fmt.Errorf("predicate %q: %q is not a boolean fact (only %s)", expr, fact, boolFactNames())
		}
		return Predicate{Kind: PredBool, Fact: fact, BoolWant: want, text: s}, nil
	}
	return Predicate{}, fmt.Errorf("predicate %q: not in the locked vocabulary (equals, in, matches, bool, subject in team, platform)", expr)
}

func requireStringFact(fact, expr string) error {
	if !stringFacts[fact] && !isLabelFact(fact) {
		return unknownFactErr(fact, expr)
	}
	return nil
}

func unknownFactErr(fact, expr string) error {
	return fmt.Errorf("predicate %q: unknown field %q (not an addressable policy axis)", expr, fact)
}

func isLabelFact(fact string) bool {
	return strings.HasPrefix(fact, "component.labels.") && len(fact) > len("component.labels.")
}

func boolFactNames() string {
	names := make([]string, 0, len(boolFacts))
	for f := range boolFacts {
		names = append(names, f)
	}
	return strings.Join(names, ", ")
}

func parseList(inner string) ([]string, error) {
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, ok := coerceLiteral(p)
		if !ok {
			return nil, fmt.Errorf("list element %q must be a quoted string, number, or bool", p)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty in[] list")
	}
	return out, nil
}

// coerceLiteral accepts a quoted string, a bare number, or true/false and
// returns its string value. It rejects a bare unquoted identifier so that a
// misspelled operator cannot silently pass as a value.
func coerceLiteral(s string) (string, bool) {
	if m := stringLiteralRE.FindStringSubmatch(s); m != nil {
		if m[1] != "" {
			return m[1], true
		}
		return m[2], true
	}
	if s == "true" || s == "false" {
		return s, true
	}
	if regexp.MustCompile(`^-?\d+(\.\d+)?$`).MatchString(s) {
		return s, true
	}
	return "", false
}

func unquote(s string) string {
	if m := stringLiteralRE.FindStringSubmatch(s); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return s
}

// InjectComponentType forces `component.type == "<typ>"` into every rule and
// rejects any rule that already constrains a foreign component.type — a
// composition fragment is constitutionally scoped to its own type
// (policy-model.md §5, SD-10). Idempotent: a rule already naming exactly typ
// is left as-is.
func (d *Document) InjectComponentType(typ string) error {
	for i := range d.Rules {
		rule := &d.Rules[i]
		already := false
		for _, p := range rule.When {
			if p.Kind == PredEquals && p.Fact == "component.type" {
				if p.Value != typ {
					return fmt.Errorf("%s: rule %q names foreign component.type %q; a composition fragment may only scope its own type %q", d.Path, rule.ID, p.Value, typ)
				}
				already = true
			}
			if p.Kind == PredIn && p.Fact == "component.type" {
				return fmt.Errorf("%s: rule %q constrains component.type via in[]; a composition fragment may only scope its own type %q", d.Path, rule.ID, typ)
			}
			if p.Kind == PredMatches && p.Fact == "component.type" {
				return fmt.Errorf("%s: rule %q constrains component.type via matches; a composition fragment may only scope its own type %q", d.Path, rule.ID, typ)
			}
		}
		if !already {
			rule.When = append(rule.When, Predicate{
				Kind:  PredEquals,
				Fact:  "component.type",
				Value: typ,
				text:  fmt.Sprintf("component.type == %q", typ),
			})
		}
	}
	return nil
}
