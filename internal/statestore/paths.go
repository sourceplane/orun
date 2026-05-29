package statestore

import (
	"fmt"
	"strings"
)

// Path-helper module — every logical path used by the new state layout is
// constructed here. Callers MUST go through the helpers; raw concatenation is
// forbidden so the validation alphabet stays enforceable at one site.
//
// All helpers return forward-slash separated, root-relative, no-leading-slash
// paths (logical paths). Drivers translate '/' to os.PathSeparator at the
// edge.
//
// Helpers in this file panic only on programmer error — specifically, when a
// caller-supplied component contains a character outside the allowed alphabet
// or contains "..", a leading "/", a backslash, or an empty segment. The
// driver's public methods catch invalid paths and surface them as ErrInvalid
// before they reach the helpers; helpers are therefore safe to call from
// internal driver code without re-checking.
//
// To assemble a path from caller-controlled strings safely, call ValidatePath
// (or ValidateComponent for a single segment) first; the public driver entry
// points already do this.

// allowedComponentRune reports whether r is part of the per-component
// alphabet: ASCII alphanumerics plus '.', '_', and '-'. The set matches
// state-store.md §2.
func allowedComponentRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}

// ValidateComponent checks that a single path segment is non-empty, does not
// equal "." or "..", and only contains characters in the allowed alphabet.
// Returns an error wrapping ErrInvalid on violation.
func ValidateComponent(component string) error {
	if component == "" {
		return fmt.Errorf("%w: empty path component", ErrInvalid)
	}
	if component == "." || component == ".." {
		return fmt.Errorf("%w: path component %q is not allowed", ErrInvalid, component)
	}
	for _, r := range component {
		if !allowedComponentRune(r) {
			return fmt.Errorf("%w: path component %q contains disallowed character %q", ErrInvalid, component, r)
		}
	}
	return nil
}

// ValidatePath checks that p is a logical path: non-empty, no leading or
// trailing '/', no Windows separator, no empty segment, and every segment
// passes ValidateComponent. Returns an error wrapping ErrInvalid on
// violation.
func ValidatePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("%w: path %q contains a backslash", ErrInvalid, p)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: path %q has a leading slash", ErrInvalid, p)
	}
	if strings.HasSuffix(p, "/") {
		return fmt.Errorf("%w: path %q has a trailing slash", ErrInvalid, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if err := ValidateComponent(seg); err != nil {
			return err
		}
	}
	return nil
}

// joinComponents validates each component and joins them with '/'. It panics
// if any component is invalid; this is intentional — the helpers below are
// only called with already-validated revKey/execKey/name values.
func joinComponents(parts ...string) string {
	for _, p := range parts {
		if err := ValidateComponent(p); err != nil {
			panic(fmt.Sprintf("statestore: internal helper called with invalid component: %v", err))
		}
	}
	return strings.Join(parts, "/")
}

// RevisionDir returns the directory that holds revision-scoped artifacts:
// "revisions/<revKey>".
func RevisionDir(revKey string) string {
	return "revisions/" + joinComponents(revKey)
}

// PlanPath returns "revisions/<revKey>/plan.json".
func PlanPath(revKey string) string {
	return RevisionDir(revKey) + "/plan.json"
}

// TriggerPath returns "revisions/<revKey>/trigger.json".
func TriggerPath(revKey string) string {
	return RevisionDir(revKey) + "/trigger.json"
}

// RevisionDocPath returns "revisions/<revKey>/revision.json".
func RevisionDocPath(revKey string) string {
	return RevisionDir(revKey) + "/revision.json"
}

// ManifestPath returns "revisions/<revKey>/manifest.json".
func ManifestPath(revKey string) string {
	return RevisionDir(revKey) + "/manifest.json"
}

// ExecutionDir returns "revisions/<revKey>/executions/<execKey>".
func ExecutionDir(revKey, execKey string) string {
	return RevisionDir(revKey) + "/executions/" + joinComponents(execKey)
}

// ExecutionDocPath returns
// "revisions/<revKey>/executions/<execKey>/execution.json".
func ExecutionDocPath(revKey, execKey string) string {
	return ExecutionDir(revKey, execKey) + "/execution.json"
}

// SnapshotPath returns
// "revisions/<revKey>/executions/<execKey>/snapshot.latest.json".
func SnapshotPath(revKey, execKey string) string {
	return ExecutionDir(revKey, execKey) + "/snapshot.latest.json"
}

// EventPath returns the per-event JSON path under an execution's events/
// directory. seq is rendered as a zero-padded 20-digit decimal so the natural
// lexicographic order of event filenames matches their sequence order; kind
// is appended after a '-' so the filename is human-scannable.
//
// Example: EventPath("rev-x", "exec-y", 7, "execution-created") returns
// "revisions/rev-x/executions/exec-y/events/00000000000000000007-execution-created.json".
//
// kind is validated with ValidateComponent so callers get the same alphabet
// guarantees as the rest of the path policy.
func EventPath(revKey, execKey string, seq uint64, kind string) string {
	return ExecutionDir(revKey, execKey) + "/events/" +
		fmt.Sprintf("%020d", seq) + "-" + joinComponents(kind) + ".json"
}

// LatestRevisionRefPath returns "refs/latest-revision.json".
func LatestRevisionRefPath() string { return "refs/latest-revision.json" }

// LatestExecutionRefPath returns "refs/latest-execution.json".
func LatestExecutionRefPath() string { return "refs/latest-execution.json" }

// TriggerLatestRefPath returns "refs/triggers/<name>/latest.json".
func TriggerLatestRefPath(name string) string {
	return "refs/triggers/" + joinComponents(name) + "/latest.json"
}

// TriggerScopeRefPath returns "refs/triggers/<name>/<scope>.json". Both name
// and scope are validated as single components — callers that need to encode
// a structured scope (e.g. a branch name with '/') should hash or otherwise
// fold it into the allowed alphabet before calling.
func TriggerScopeRefPath(name, scope string) string {
	return "refs/triggers/" + joinComponents(name) + "/" + joinComponents(scope) + ".json"
}

// NamedRefPath returns "refs/named/<name>.json".
func NamedRefPath(name string) string {
	return "refs/named/" + joinComponents(name) + ".json"
}

// RevisionIndexPath returns "indexes/revisions/<revKey>.json".
func RevisionIndexPath(revKey string) string {
	return "indexes/revisions/" + joinComponents(revKey) + ".json"
}

// ExecutionIndexPath returns "indexes/executions/<execKey>.json".
func ExecutionIndexPath(execKey string) string {
	return "indexes/executions/" + joinComponents(execKey) + ".json"
}
