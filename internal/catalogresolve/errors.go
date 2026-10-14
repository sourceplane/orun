package catalogresolve

import (
	"fmt"
	"sort"
	"strings"
)

// Severity is the severity of a ValidationIssue. resolution-pipeline.md
// §6 defines a per-rule default that the `--strict` flag promotes from
// Warning to Error.
type Severity int

const (
	// SeverityWarning is collected but does not abort the resolver.
	SeverityWarning Severity = iota
	// SeverityError aborts the resolver as soon as it is appended.
	SeverityError
)

// String returns the lowercase name of the severity for log/JSON use.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return fmt.Sprintf("severity(%d)", int(s))
	}
}

// ValidationIssue is one entry in the [].ValidationIssue list returned
// alongside the ResolvedCatalog. Every issue carries enough context for
// the writer (C4) and CLI (C5) to render a human-readable line and to
// persist the issue under `catalog.json.validation` per
// resolution-pipeline.md §8.
type ValidationIssue struct {
	// File is the workspace-relative source the issue is attributed
	// to. Empty for repo-wide issues (e.g. duplicate component keys
	// where the offending pair is recorded in Detail).
	File string
	// Pointer is an RFC 6901 JSON pointer into the offending field
	// when the issue is field-scoped. Empty when not applicable.
	Pointer string
	// Severity is the runtime severity after `--strict` promotion.
	Severity Severity
	// Code is a stable machine-readable identifier (e.g.
	// "component.metadata.owner.missing"). Stable across versions so
	// downstream filters can match.
	Code string
	// Message is a human-readable one-liner.
	Message string
	// Detail is an optional structured payload (e.g. cycle path,
	// duplicate component paths). May be nil.
	Detail map[string]any
}

// Error returns the issue rendered as a single-line string. Provided so
// a single ValidationIssue may be unwrapped from the `error` channel
// when the resolver aborts on a strict violation.
func (v ValidationIssue) Error() string {
	var b strings.Builder
	b.WriteString("catalogresolve: ")
	b.WriteString(v.Severity.String())
	if v.Code != "" {
		b.WriteByte(' ')
		b.WriteString(v.Code)
	}
	if v.File != "" {
		b.WriteString(" at ")
		b.WriteString(v.File)
		if v.Pointer != "" {
			b.WriteByte('#')
			b.WriteString(v.Pointer)
		}
	}
	if v.Message != "" {
		b.WriteString(": ")
		b.WriteString(v.Message)
	}
	return b.String()
}

// ErrComponentInvalid is returned when a resolved component fails a
// hard validation rule (e.g. missing metadata.name in default mode).
type ErrComponentInvalid struct {
	Path   string
	Reason string
}

func (e *ErrComponentInvalid) Error() string {
	return fmt.Sprintf("catalogresolve: component %s invalid: %s", e.Path, e.Reason)
}

// ErrDuplicateComponent reports two or more authored component.yaml
// files that resolve to the same componentKey (the same metadata.name
// within one namespace and repo). Paths are sorted workspace-relative
// slash paths. Resolve returns it in place of the bare
// component.key.duplicate issue so a caller can name every offending
// file; the issue itself is still recorded and reachable via Unwrap.
type ErrDuplicateComponent struct {
	// Key is the colliding componentKey (`<namespace>/<repo>/<name>`).
	Key string
	// Name is the authored metadata.name the files share. May be empty
	// when the error is built without the authored manifests.
	Name string
	// Paths are the sorted workspace-relative manifest paths.
	Paths []string
	// Issue is the component.key.duplicate validation issue this error
	// stands in for, when it came out of the validate stage.
	Issue *ValidationIssue
}

func (e *ErrDuplicateComponent) Error() string {
	what := "component " + e.Key
	if e.Name != "" {
		what = fmt.Sprintf("component name %q (componentKey %s)", e.Name, e.Key)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "catalogresolve: duplicate %s", what)
	switch len(e.Paths) {
	case 0:
	case 1:
		fmt.Fprintf(&b, " declared in %s", e.Paths[0])
	case 2:
		fmt.Fprintf(&b, " declared in both %s and %s", e.Paths[0], e.Paths[1])
	default:
		fmt.Fprintf(&b, " declared in %d manifests: %s", len(e.Paths), strings.Join(e.Paths, ", "))
	}
	b.WriteString("; every component manifest discovery finds must have a unique metadata.name (exclude copies with catalog.discovery.exclude, or narrow discovery.roots)")
	return b.String()
}

// Unwrap exposes the underlying validation issue so errors.As against a
// ValidationIssue keeps working for callers that match on Code.
func (e *ErrDuplicateComponent) Unwrap() error {
	if e.Issue == nil {
		return nil
	}
	return *e.Issue
}

// duplicateComponentError turns a component.key.duplicate issue into the
// typed error, reading the key and paths back out of its Detail payload.
// name is the authored metadata.name shared by the manifests (may be "").
func duplicateComponentError(issue ValidationIssue, name string) *ErrDuplicateComponent {
	e := &ErrDuplicateComponent{Name: name}
	cp := issue
	e.Issue = &cp
	if k, ok := issue.Detail["componentKey"].(string); ok {
		e.Key = k
	}
	if ps, ok := issue.Detail["paths"].([]string); ok {
		e.Paths = append([]string(nil), ps...)
	}
	sort.Strings(e.Paths)
	return e
}

// ErrDependencyMissing reports a `spec.dependsOn[*].component`
// reference whose target is absent from the discovered component set.
// `From` is the source component key; `To` is the unresolved reference
// as authored.
type ErrDependencyMissing struct {
	From string
	To   string
}

func (e *ErrDependencyMissing) Error() string {
	return fmt.Sprintf("catalogresolve: dependency missing: %s → %s", e.From, e.To)
}

// ErrCycle reports a cycle in either the `calls` graph or the
// `deploy-after` graph. Path is the offending cycle in author-order
// (a → b → c → a); EdgeType is one of {"calls", "deploy-after",
// "depends-on", "links-to"}.
type ErrCycle struct {
	Path     []string
	EdgeType string
}

func (e *ErrCycle) Error() string {
	return fmt.Sprintf("catalogresolve: cycle in %s edges: %v", e.EdgeType, e.Path)
}

// ErrInferenceFailed wraps a non-fatal inference miss. In default mode
// the resolver logs and skips; the wrapper exists so test code can
// match the error class without coupling to log content.
type ErrInferenceFailed struct {
	Path       string
	Reason     string
	Underlying error
}

func (e *ErrInferenceFailed) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("catalogresolve: inference failed at %s: %s: %v", e.Path, e.Reason, e.Underlying)
	}
	return fmt.Sprintf("catalogresolve: inference failed at %s: %s", e.Path, e.Reason)
}

func (e *ErrInferenceFailed) Unwrap() error { return e.Underlying }

// ErrResolverInternal is the bug bucket for unexpected stage failures.
// Stage is the integer stage number from resolution-pipeline.md §1
// (e.g. 6 = inference, 9 = validate, 10 = manifestHash).
type ErrResolverInternal struct {
	Stage      int
	Underlying error
}

func (e *ErrResolverInternal) Error() string {
	return fmt.Sprintf("catalogresolve: internal error at stage %d: %v", e.Stage, e.Underlying)
}

func (e *ErrResolverInternal) Unwrap() error { return e.Underlying }
