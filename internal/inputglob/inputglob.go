// Package inputglob validates and matches a component's `spec.change.inputs` globs:
// repository-root-relative, doublestar-style patterns (`**` crosses directory
// boundaries) that let a component claim files outside its own directory for
// change detection — a root lockfile, turbo.json, or a shared tooling tree.
//
// Both component.yaml parsers (internal/model for the plan engine and
// internal/catalogmodel for the catalog) validate with Validate, and the change
// engine (internal/affected) matches with Match, so an accepted pattern means
// the same thing everywhere.
package inputglob

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Validate reports why pattern is not an acceptable input glob, or nil. A
// pattern must be non-empty, repository-root-relative (no leading "/", no
// drive letter, no backslashes), free of empty, "." and ".." segments, and a
// syntactically valid doublestar glob.
func Validate(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("input glob must not be empty")
	}
	if pattern != strings.TrimSpace(pattern) {
		return fmt.Errorf("input glob %q must not have leading or trailing whitespace", pattern)
	}
	if strings.Contains(pattern, `\`) {
		return fmt.Errorf("input glob %q must use '/' separators (backslashes are not allowed)", pattern)
	}
	if strings.HasPrefix(pattern, "/") || hasDriveLetter(pattern) {
		return fmt.Errorf("input glob %q must be relative to the repository root, not absolute", pattern)
	}
	for _, seg := range strings.Split(pattern, "/") {
		switch seg {
		case "":
			return fmt.Errorf("input glob %q must not contain empty path segments or a trailing '/'", pattern)
		case ".", "..":
			return fmt.Errorf("input glob %q must not contain '.' or '..' segments", pattern)
		}
	}
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("input glob %q is not a valid glob pattern", pattern)
	}
	return nil
}

// ValidateAll validates every pattern and returns the first error, prefixed
// with the offending index.
func ValidateAll(patterns []string) error {
	for i, p := range patterns {
		if err := Validate(p); err != nil {
			return fmt.Errorf("inputs[%d]: %w", i, err)
		}
	}
	return nil
}

// Match reports whether the repository-relative path matches pattern. The path
// is normalized the way the change engine normalizes changed files ('\' → '/',
// a leading "./" dropped). A malformed pattern never matches.
func Match(pattern, path string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, `\`, "/"), "./")
	if path == "" {
		return false
	}
	ok, err := doublestar.Match(pattern, path)
	return err == nil && ok
}

// MatchAny reports whether path matches any of patterns and returns the first
// matching pattern.
func MatchAny(patterns []string, path string) (string, bool) {
	for _, p := range patterns {
		if Match(p, path) {
			return p, true
		}
	}
	return "", false
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}
