// Package secretref implements the secret:// reference grammar — the only
// secret-shaped thing that may appear in intent, plans, or any content-
// addressed object. A reference carries no value and no identity; resolution
// happens exclusively in the runner against the backend
// (specs/orun-secrets/data-model.md §1).
//
//	secret://<workspace>/<project>/<env>/<KEY>[@<version>]
package secretref

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scheme is the URI scheme prefix for secret references.
const Scheme = "secret://"

var (
	// keyRE matches the KEY segment — the same shape the platform's
	// config-worker enforces.
	keyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
	// slugRE matches workspace/project/environment slugs.
	slugRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
)

// Ref is a parsed secret reference.
type Ref struct {
	// Workspace is the workspace slug (an org in the account).
	Workspace string
	// Project is the project slug (== the repo).
	Project string
	// Env is the environment slug declared in intent.
	Env string
	// Key is the secret key within the project's chain.
	Key string
	// Version pins a specific version; 0 means head-at-resolve-time.
	Version int
}

// IsRef reports whether s is secret-reference-shaped (has the scheme prefix).
// It does not validate the body — use Parse for that. This is the leak-guard
// primitive: anything in a secretEnv slot that is not IsRef is a literal.
func IsRef(s string) bool {
	return strings.HasPrefix(s, Scheme)
}

// Parse parses and validates a secret:// reference.
//
// Error messages never echo the input: a failing value in a secret-shaped
// slot may BE a pasted secret, and errors end up in logs and CI output.
// Callers identify the offending slot by key (which they know), not by value.
func Parse(s string) (Ref, error) {
	if !IsRef(s) {
		return Ref{}, fmt.Errorf("not a secret reference (want %s<workspace>/<project>/<env>/<KEY>[@<version>], got a literal)", Scheme)
	}
	body := strings.TrimPrefix(s, Scheme)

	version := 0
	if at := strings.LastIndex(body, "@"); at >= 0 {
		v, err := strconv.Atoi(body[at+1:])
		if err != nil || v < 1 {
			return Ref{}, fmt.Errorf("invalid secret reference: @<version> must be a positive integer")
		}
		version = v
		body = body[:at]
	}

	parts := strings.Split(body, "/")
	if len(parts) != 4 {
		return Ref{}, fmt.Errorf("invalid secret reference: want %s<workspace>/<project>/<env>/<KEY>[@<version>]", Scheme)
	}

	ref := Ref{
		Workspace: parts[0],
		Project:   parts[1],
		Env:       parts[2],
		Key:       parts[3],
		Version:   version,
	}
	for _, seg := range []struct{ name, val string }{
		{"workspace", ref.Workspace},
		{"project", ref.Project},
		{"env", ref.Env},
	} {
		if !slugRE.MatchString(seg.val) {
			return Ref{}, fmt.Errorf("invalid secret reference: bad %s segment", seg.name)
		}
	}
	if !keyRE.MatchString(ref.Key) {
		return Ref{}, fmt.Errorf("invalid secret reference: bad key segment")
	}
	return ref, nil
}

// String renders the canonical reference form.
func (r Ref) String() string {
	s := Scheme + r.Workspace + "/" + r.Project + "/" + r.Env + "/" + r.Key
	if r.Version > 0 {
		s += "@" + strconv.Itoa(r.Version)
	}
	return s
}
