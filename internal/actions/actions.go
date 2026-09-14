// Package actions is orun's closed, versioned registry of typed actions a
// Blueprint hook may call (orun-bootstrap-engine BE-O1).
//
// # Why a registry and not an argv
//
// A hook could always say `run: ["orun", "pr", "land", …]`. Five things are
// different when it says `uses: orun.pr/land@v1` instead, and all five are the
// reason this package exists:
//
//  1. A misspelled parameter is a BLUEPRINT VALIDATION ERROR naming the line,
//     not an exit code halfway through someone's bootstrap.
//  2. The action is resolved in-process, so nothing needs `orun` on PATH.
//  3. Outputs are typed and addressable by a later hook. An argv hook has no
//     outputs at all — the runner sends its stdout to stderr and discards it.
//  4. A failure is a typed error a surface can render, not an exit status.
//  5. The set is CLOSED, which is a real audit boundary: hooks run OUTSIDE the
//     template sandbox, so "any argv at all" is the widest possible surface and
//     "one of these eight" is a reviewable one.
//
// `run:` still exists, for genuine ecosystem escapes. That is the invariant
// held exactly: an action is orun's own surface, argv is how a blueprint
// reaches a tool orun does not and should not know about.
//
// # Why this package, and not internal/scaffold
//
// The scaffold package's render core is structurally forbidden from importing
// os/exec/net/time/rand, and its ecosystem-neutrality test forbids naming any
// specific vendor. Actions need exactly those things. So actions live here and
// are INJECTED into the engine through an interface — the engine calls a
// Runner, it does not import an implementation.
package actions

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ParamType is the closed set of parameter types an action may declare.
type ParamType string

const (
	ParamString     ParamType = "string"
	ParamBool       ParamType = "bool"
	ParamInt        ParamType = "int"
	ParamStringList ParamType = "stringList"
)

// Param is one declared parameter of an action.
type Param struct {
	Name        string
	Type        ParamType
	Required    bool
	Description string
	// Default is applied when the hook omits the parameter. Nil means none.
	Default any
}

// Spec is an action's declared contract: what it is called, what it takes and
// what it hands back. It is the whole of what a blueprint author can rely on.
type Spec struct {
	ID      string
	Summary string
	Params  []Param
	// Outputs names the keys Result.Outputs may carry. Declared so a blueprint
	// referencing an output can be checked before anything runs.
	Outputs []string
}

// Param returns the named parameter's declaration.
func (s Spec) Param(name string) (Param, bool) {
	for _, p := range s.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// ParamNames returns the declared parameter names, sorted — for error messages
// that tell the author what they could have written.
func (s Spec) ParamNames() []string {
	out := make([]string, 0, len(s.Params))
	for _, p := range s.Params {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// HasOutput reports whether the spec declares the named output.
func (s Spec) HasOutput(name string) bool {
	for _, o := range s.Outputs {
		if o == name {
			return true
		}
	}
	return false
}

// Input is what an action is handed at run time.
type Input struct {
	// Dir is the directory the action operates in — the product tree.
	Dir string
	// BaseDir is the BASELINE's own directory — where the blueprint that
	// declared this hook lives. It is not the same place as Dir and the
	// difference is load-bearing: a baseline's machinery (a task contract, a
	// policy document) is authored beside the blueprint and is deliberately
	// not copied into the product, so a parameter naming one of those files
	// has to resolve here. Empty when the caller has no blueprint directory,
	// in which case a relative path falls back to Dir.
	BaseDir string
	// Params are the resolved parameters: declared defaults applied, and every
	// template expression already rendered.
	Params map[string]any
}

// PathParam resolves a path parameter the way a baseline's author means it:
// relative to the BASELINE (BaseDir), because the files a hook names — a task
// contract, a policy — are the baseline's, not the product's. An absolute path
// is taken as given, and an empty BaseDir falls back to Dir so a caller that
// drives an action directly still resolves something sensible.
func PathParam(in Input, name string) string {
	raw := strings.TrimSpace(StringParam(in, name))
	if raw == "" || filepath.IsAbs(raw) {
		return raw
	}
	base := in.BaseDir
	if base == "" {
		base = in.Dir
	}
	if base == "" {
		return raw
	}
	return filepath.Join(base, filepath.FromSlash(raw))
}

// Result is what an action hands back.
type Result struct {
	// Outputs are readable by later hooks in the same phase.
	Outputs map[string]string
	// Pending, when set, means the action is waiting rather than done or
	// failed — see Pending.
	Pending *Pending
}

// RunFunc executes one action.
type RunFunc func(ctx context.Context, in Input) (Result, error)

type entry struct {
	spec Spec
	run  RunFunc
}

var registry = map[string]entry{}

// idPattern is the action naming grammar: <namespace>/<verb>@v<major>.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9.]*/[a-z][a-z0-9-]*@v[0-9]+$`)

// register adds an action to the closed set. Called only from this package's
// init functions; a duplicate or malformed id is a programming error and panics
// at startup rather than shipping a registry that lies about itself.
func register(s Spec, fn RunFunc) {
	if !idPattern.MatchString(s.ID) {
		panic(fmt.Sprintf("actions: malformed action id %q", s.ID))
	}
	if _, dup := registry[s.ID]; dup {
		panic(fmt.Sprintf("actions: duplicate action id %q", s.ID))
	}
	registry[s.ID] = entry{spec: s, run: fn}
}

// Lookup returns the declared contract for an action id.
func Lookup(id string) (Spec, bool) {
	e, ok := registry[id]
	return e.spec, ok
}

// IDs lists every registered action, sorted.
func IDs() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// templated reports whether a value is a string carrying a template
// expression. Such a value cannot be type-checked before placement — its type
// is whatever the expression resolves to — so validation checks only that the
// parameter is DECLARED and defers the rest to Resolve at run time.
//
// This is the one deliberate hole in parse-time checking, and it is narrow: a
// misspelled parameter NAME is still caught, which is the mistake that actually
// happens. A well-named parameter whose template resolves to the wrong type is
// caught at run time, before the action does anything.
func templated(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "{{")
}

// Validate checks a hook's `with:` block against an action's declared
// parameters. It reports the FIRST problem, because a blueprint author fixes
// them one at a time and a list of five is not more useful than the first.
func Validate(id string, with map[string]any) error {
	spec, ok := Lookup(id)
	if !ok {
		return fmt.Errorf("unknown action %q (known actions: %s)", id, strings.Join(IDs(), ", "))
	}
	for name, value := range with {
		p, known := spec.Param(name)
		if !known {
			return fmt.Errorf("action %s has no parameter %q (it takes: %s)", id, name, strings.Join(spec.ParamNames(), ", "))
		}
		if templated(value) {
			continue
		}
		if err := checkType(p, value); err != nil {
			return fmt.Errorf("action %s parameter %q: %w", id, name, err)
		}
	}
	for _, p := range spec.Params {
		if !p.Required {
			continue
		}
		if _, given := with[p.Name]; !given {
			return fmt.Errorf("action %s requires parameter %q (%s)", id, p.Name, p.Description)
		}
	}
	return nil
}

// checkType enforces one parameter's declared type against a concrete value.
// YAML decoding is generous about numbers, so an int parameter accepts any
// integral value; a fractional one is refused rather than silently truncated.
func checkType(p Param, value any) error {
	switch p.Type {
	case ParamString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expects a string, got %T", value)
		}
	case ParamBool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expects a boolean, got %T", value)
		}
	case ParamInt:
		switch n := value.(type) {
		case int:
		case int64:
		case float64:
			if n != float64(int64(n)) {
				return fmt.Errorf("expects a whole number, got %v", n)
			}
		default:
			return fmt.Errorf("expects a number, got %T", value)
		}
	case ParamStringList:
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expects a list of strings, got %T", value)
		}
		for i, item := range items {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("expects a list of strings; item %d is %T", i, item)
			}
		}
	default:
		return fmt.Errorf("declares unknown type %q", p.Type)
	}
	return nil
}

// Resolve applies declared defaults and re-checks every value's type. It runs
// AFTER template expressions have been rendered, so this is where a parameter
// whose template resolved to the wrong shape is caught — before the action runs.
func Resolve(id string, with map[string]any) (map[string]any, error) {
	spec, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown action %q", id)
	}
	out := make(map[string]any, len(spec.Params))
	for _, p := range spec.Params {
		value, given := with[p.Name]
		if !given {
			if p.Default != nil {
				out[p.Name] = p.Default
			}
			continue
		}
		if err := checkType(p, value); err != nil {
			return nil, fmt.Errorf("action %s parameter %q: %w", id, p.Name, err)
		}
		out[p.Name] = value
	}
	return out, nil
}

// Run resolves and executes an action.
func Run(ctx context.Context, id string, in Input) (Result, error) {
	e, ok := registry[id]
	if !ok {
		return Result{}, fmt.Errorf("unknown action %q", id)
	}
	params, err := Resolve(id, in.Params)
	if err != nil {
		return Result{}, err
	}
	in.Params = params
	res, err := e.run(ctx, in)
	if err != nil {
		return res, err
	}
	if res.Pending != nil {
		return res, &PendingError{ID: id, Pending: *res.Pending}
	}
	return res, nil
}

// String helpers for action implementations: the params map is already
// type-checked by Resolve, so these are total for a declared parameter.

// StringParam reads a declared string parameter.
func StringParam(in Input, name string) string {
	s, _ := in.Params[name].(string)
	return s
}

// IntParam reads a declared int parameter, normalizing YAML's number types.
func IntParam(in Input, name string) int {
	switch n := in.Params[name].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// BoolParam reads a declared bool parameter.
func BoolParam(in Input, name string) bool {
	b, _ := in.Params[name].(bool)
	return b
}

// StringListParam reads a declared stringList parameter.
func StringListParam(in Input, name string) []string {
	items, _ := in.Params[name].([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Pending is an action reporting that it is not finished and not failed
// (orun-bootstrap-engine BE-O4).
//
// A convergence is still running; a provider connection has not been made yet.
// Neither is an error — nothing is wrong — and neither is success. Collapsing
// them into one or the other is how an unattended bootstrap either hangs or
// reports a product live that is not.
type Pending struct {
	// Reason is shown to a person. It should say what is being waited on.
	Reason string
	// RetryAfter is how long the action suggests waiting before asking again.
	// Zero means the caller decides.
	RetryAfter time.Duration
}

// PendingError carries a Pending out through the error channel, so every
// existing caller of an action keeps its two-value signature and only callers
// that care about waiting have to know the type exists.
type PendingError struct {
	ID string
	Pending
}

func (e *PendingError) Error() string {
	if e.Reason == "" {
		return e.ID + ": pending"
	}
	return e.ID + ": pending — " + e.Reason
}

// IsPending reports whether err is an action parking rather than failing.
func IsPending(err error) (*PendingError, bool) {
	var pe *PendingError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
