// Package intentpolicy parses and enforces the typed policies a platform team
// declares on intent groups and environments (`groups.<name>.policies`,
// `environments.<name>.policies`, including those merged in from intent
// presets) together with the `policies` block of an ExecutionProfile.
//
// The compile-time policies are checked while the intent is expanded into
// component instances, so `orun validate` and `orun plan` fail on a violation.
// The runtime policies (requireCleanGitTree, requireApproval) are recorded on
// every plan job and enforced by `orun run`. Either way the effective set lands
// on the plan job, which keeps the plan the audit artifact.
//
// The vocabulary is closed: an unknown key is a violation, so a typo can never
// silently disable a guardrail.
package intentpolicy

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// Policy keys accepted on group and environment `policies` maps.
const (
	KeyPinnedParameters              = "pinnedParameters"
	KeyRequireProfile                = "requireProfile"
	KeyRequirePinnedTerraformVersion = "requirePinnedTerraformVersion"
	KeyRequireCleanGitTree           = "requireCleanGitTree"
	KeyRequireApproval               = "requireApproval"
)

// KnownKeys lists every accepted policy key, sorted.
var KnownKeys = []string{
	KeyPinnedParameters,
	KeyRequireApproval,
	KeyRequireCleanGitTree,
	KeyRequirePinnedTerraformVersion,
	KeyRequireProfile,
}

// TerraformVersionParameter is the component parameter
// requirePinnedTerraformVersion inspects.
const TerraformVersionParameter = "terraformVersion"

// Set is the typed form of one layer's `policies` map.
type Set struct {
	// PinnedParameters is keyed like parameterDefaults: a composition type or
	// "*" for every type, then parameter name → pinned value.
	PinnedParameters              map[string]map[string]interface{}
	RequireProfile                []string
	RequirePinnedTerraformVersion bool
	RequireCleanGitTree           bool
	RequireApproval               bool
}

// Layer is a parsed policy set plus the place that declared it, e.g.
// "group:platform" or "environment:production".
type Layer struct {
	Source string
	Set    Set
}

// GroupSource and EnvironmentSource name a layer for violations and the plan.
func GroupSource(name string) string       { return "group:" + name }
func EnvironmentSource(name string) string { return "environment:" + name }
func ProfileSource(name string) string     { return "profile:" + name }

// Violation is one broken policy. Component and Environment are empty for a
// malformed declaration that is not tied to an instance.
type Violation struct {
	Policy      string `json:"policy"`
	Source      string `json:"source"`
	Component   string `json:"component,omitempty"`
	Environment string `json:"environment,omitempty"`
	Message     string `json:"message"`
}

func (v Violation) String() string {
	var b strings.Builder
	if v.Component != "" {
		fmt.Fprintf(&b, "component %s (env %s): ", v.Component, v.Environment)
	}
	fmt.Fprintf(&b, "%s [%s]: %s", v.Policy, v.Source, v.Message)
	return b.String()
}

// Error is the structured error `orun validate` and `orun plan` return when a
// policy is malformed or violated. Violations are sorted for stable output.
type Error struct {
	Violations []Violation
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		lines[i] = v.String()
	}
	noun := "violations"
	if len(lines) == 1 {
		noun = "violation"
	}
	return fmt.Sprintf("policy check failed (%d %s):\n  - %s", len(lines), noun, strings.Join(lines, "\n  - "))
}

// NewError returns nil for no violations, else a sorted *Error.
func NewError(violations []Violation) error {
	if len(violations) == 0 {
		return nil
	}
	sorted := append([]Violation(nil), violations...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		if a.Environment != b.Environment {
			return a.Environment < b.Environment
		}
		if a.Policy != b.Policy {
			return a.Policy < b.Policy
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Message < b.Message
	})
	return &Error{Violations: sorted}
}

// Parse converts one layer's raw `policies` map into a Set. Unknown keys and
// ill-typed values are violations.
func Parse(source string, raw map[string]interface{}) (Set, []Violation) {
	var set Set
	var violations []Violation
	bad := func(policy, format string, args ...interface{}) {
		violations = append(violations, Violation{Policy: policy, Source: source, Message: fmt.Sprintf(format, args...)})
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := raw[key]
		switch key {
		case KeyRequireApproval, KeyRequireCleanGitTree, KeyRequirePinnedTerraformVersion:
			b, ok := parseBool(value)
			if !ok {
				bad(key, "must be a boolean, got %v", describe(value))
				continue
			}
			switch key {
			case KeyRequireApproval:
				set.RequireApproval = b
			case KeyRequireCleanGitTree:
				set.RequireCleanGitTree = b
			case KeyRequirePinnedTerraformVersion:
				set.RequirePinnedTerraformVersion = b
			}
		case KeyRequireProfile:
			patterns, ok := parseStrings(value)
			if !ok || len(patterns) == 0 {
				bad(key, "must be a profile name or a non-empty list of names (glob patterns allowed), got %v", describe(value))
				continue
			}
			for _, p := range patterns {
				if _, err := path.Match(p, ""); err != nil {
					bad(key, "invalid pattern %q: %v", p, err)
				}
			}
			set.RequireProfile = patterns
		case KeyPinnedParameters:
			pins, msg := parsePins(value)
			if msg != "" {
				bad(key, "%s", msg)
				continue
			}
			set.PinnedParameters = pins
		default:
			bad(key, "unknown policy (known: %s)", strings.Join(KnownKeys, ", "))
		}
	}
	return set, violations
}

func parseBool(v interface{}) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func parseStrings(v interface{}) ([]string, bool) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, false
		}
		return []string{t}, true
	case []string:
		return t, true
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func parsePins(v interface{}) (map[string]map[string]interface{}, string) {
	outer, ok := asStringMap(v)
	if !ok {
		return nil, fmt.Sprintf(`must map a composition type (or "*") to parameter → value, got %s`, describe(v))
	}
	pins := make(map[string]map[string]interface{}, len(outer))
	for typ, inner := range outer {
		params, ok := asStringMap(inner)
		if !ok {
			return nil, fmt.Sprintf("%s: must map parameter → value, got %s", typ, describe(inner))
		}
		for name := range params {
			if name == "path" {
				return nil, fmt.Sprintf("%s: path cannot be pinned (it is not a parameter)", typ)
			}
		}
		pins[typ] = params
	}
	return pins, ""
}

func asStringMap(v interface{}) (map[string]interface{}, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		return t, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}

func describe(v interface{}) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%T %v", v, v)
}

// Instance is what Enforce needs to know about one component instance.
type Instance struct {
	Component   string
	Environment string
	Type        string
	// ProfileChecked is true when execution profiles were resolved (a
	// composition registry was available); requireProfile is only checked then.
	ProfileChecked bool
	// ProfileName is the profile's name within its composition (what a
	// subscription's `profile:` and requireProfile patterns use); ProfileRef
	// is its qualified ref (e.g. terraform.release), used to label the source.
	ProfileName     string
	ProfileRef      string
	ProfilePolicies *model.ProfilePolicies
	// ComponentParameters and SubscriptionParameters are what the component
	// itself declared (component.yaml parameters, then the subscription's).
	// A value here that differs from a pin is an override, i.e. a violation.
	ComponentParameters    map[string]interface{}
	SubscriptionParameters map[string]interface{}
	// Parameters are the merged parameters; Enforce writes pinned values here.
	Parameters map[string]interface{}
	// Interpolate expands {{ .environment }}-style variables in a string.
	Interpolate func(string) string
}

var exactVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// IsExactVersion reports whether v names one exact release (1.9.8, v1.9.8,
// 1.10.0-rc1), not a range, wildcard, or "latest".
func IsExactVersion(v string) bool { return exactVersion.MatchString(strings.TrimSpace(v)) }

// Enforce applies the layers (and the instance's profile policies) to one
// instance: it writes pinned parameter values into inst.Parameters, checks
// every compile-time policy, and returns the effective set to record on the
// plan (nil when no policy applies).
func Enforce(layers []Layer, inst Instance) (*model.PlanPolicies, []Violation) {
	eff := &model.PlanPolicies{}
	var violations []Violation
	fail := func(policy, source, format string, args ...interface{}) {
		violations = append(violations, Violation{
			Policy: policy, Source: source, Component: inst.Component, Environment: inst.Environment,
			Message: fmt.Sprintf(format, args...),
		})
	}
	addSource := func(key, source string) {
		if eff.Sources == nil {
			eff.Sources = map[string][]string{}
		}
		for _, s := range eff.Sources[key] {
			if s == source {
				return
			}
		}
		eff.Sources[key] = append(eff.Sources[key], source)
	}
	interp := func(v interface{}) interface{} {
		if s, ok := v.(string); ok && inst.Interpolate != nil {
			return inst.Interpolate(s)
		}
		return v
	}

	// Booleans: a union across layers.
	for _, l := range layers {
		if l.Set.RequireApproval {
			eff.RequireApproval = true
			addSource(KeyRequireApproval, l.Source)
		}
		if l.Set.RequireCleanGitTree {
			eff.RequireCleanGitTree = true
			addSource(KeyRequireCleanGitTree, l.Source)
		}
		if l.Set.RequirePinnedTerraformVersion {
			eff.RequirePinnedTerraformVersion = true
			addSource(KeyRequirePinnedTerraformVersion, l.Source)
		}
	}
	profileSource := ProfileSource(inst.ProfileName)
	if inst.ProfileRef != "" {
		profileSource = ProfileSource(inst.ProfileRef)
	}
	tfFromProfile := false
	if p := inst.ProfilePolicies; p != nil {
		if p.RequireApproval {
			eff.RequireApproval = true
			addSource(KeyRequireApproval, profileSource)
		}
		if p.RequireCleanGitTree {
			eff.RequireCleanGitTree = true
			addSource(KeyRequireCleanGitTree, profileSource)
		}
		if p.RequirePinnedTerraformVersion {
			eff.RequirePinnedTerraformVersion = true
			tfFromProfile = true
			addSource(KeyRequirePinnedTerraformVersion, profileSource)
		}
	}

	// Pinned parameters. Within a layer the type-specific pin beats "*";
	// across layers two different pins for one parameter are a conflict.
	pinSource := map[string]string{}
	for _, l := range layers {
		layerPins := map[string]interface{}{}
		for _, typ := range []string{"*", inst.Type} {
			for name, v := range l.Set.PinnedParameters[typ] {
				layerPins[name] = interp(v)
			}
		}
		for _, name := range sortedKeys(layerPins) {
			v := layerPins[name]
			if prev, ok := eff.PinnedParameters[name]; ok {
				if !sameValue(prev, v) {
					fail(KeyPinnedParameters+"."+name, l.Source, "conflicts with %s, which pins %s; this layer pins %s", pinSource[name], show(prev), show(v))
				}
				continue
			}
			if eff.PinnedParameters == nil {
				eff.PinnedParameters = map[string]interface{}{}
			}
			eff.PinnedParameters[name] = v
			pinSource[name] = l.Source
			addSource(KeyPinnedParameters, l.Source)
		}
	}
	for _, name := range sortedKeys(eff.PinnedParameters) {
		pinned := eff.PinnedParameters[name]
		for _, decl := range []struct {
			where  string
			params map[string]interface{}
		}{{"component", inst.ComponentParameters}, {"subscription", inst.SubscriptionParameters}} {
			if v, ok := decl.params[name]; ok && !sameValue(interp(v), pinned) {
				fail(KeyPinnedParameters+"."+name, pinSource[name], "%s sets %s = %s, but the policy pins it to %s", decl.where, name, show(interp(v)), show(pinned))
			}
		}
		if inst.Parameters != nil {
			inst.Parameters[name] = pinned
		}
	}

	// Required profile: every declaring layer must be satisfied.
	for _, l := range layers {
		if len(l.Set.RequireProfile) == 0 {
			continue
		}
		eff.RequireProfile = appendUnique(eff.RequireProfile, l.Set.RequireProfile...)
		addSource(KeyRequireProfile, l.Source)
		if !inst.ProfileChecked {
			continue
		}
		if inst.ProfileName == "" {
			fail(KeyRequireProfile, l.Source, "resolves no execution profile; required one of %s", strings.Join(l.Set.RequireProfile, ", "))
			continue
		}
		if !matchesAnyGlob(l.Set.RequireProfile, inst.ProfileName) {
			fail(KeyRequireProfile, l.Source, "resolves profile %q; required one of %s", inst.ProfileName, strings.Join(l.Set.RequireProfile, ", "))
		}
	}

	// Pinned terraform version: checked whenever the component carries the
	// parameter; required to be present when a profile demands it (a profile
	// is composition-specific, so it knows the composition runs terraform).
	if eff.RequirePinnedTerraformVersion {
		source := strings.Join(eff.Sources[KeyRequirePinnedTerraformVersion], ", ")
		raw, present := inst.Parameters[TerraformVersionParameter]
		switch {
		case present:
			s, _ := raw.(string)
			if !IsExactVersion(s) {
				fail(KeyRequirePinnedTerraformVersion, source, "%s is %s; must be an exact version such as 1.9.8 (no ranges, wildcards, or latest)", TerraformVersionParameter, show(raw))
			}
		case tfFromProfile:
			fail(KeyRequirePinnedTerraformVersion, source, "%s is not set; the profile requires an exact terraform version", TerraformVersionParameter)
		}
	}

	if eff.IsZero() {
		return nil, violations
	}
	for k := range eff.Sources {
		sort.Strings(eff.Sources[k])
	}
	return eff, violations
}

func matchesAnyGlob(patterns []string, value string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, value); ok {
			return true
		}
	}
	return false
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sameValue compares two decoded YAML/JSON values by their canonical JSON
// encoding, so maps compare regardless of order and 3 never equals "3".
func sameValue(a, b interface{}) bool {
	ja, errA := json.Marshal(normalize(a))
	jb, errB := json.Marshal(normalize(b))
	if errA != nil || errB != nil {
		return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b)
	}
	return string(ja) == string(jb)
}

func normalize(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = normalize(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

func show(v interface{}) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	if b, err := json.Marshal(normalize(v)); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}
