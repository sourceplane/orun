// Package codeowners parses a GitHub-style CODEOWNERS file into a matcher that
// resolves a workspace-relative path to its owner(s). It is the input that lets
// the catalog resolver derive ownership from the repo's real source of truth
// (orun-service-catalog/design.md §4.3, S-2) without coupling the pure resolver
// to the filesystem — the caller reads + parses CODEOWNERS and hands the matcher
// to the catalog build step.
//
// Semantics follow GitHub's CODEOWNERS:
//   - one rule per non-comment, non-blank line: `<pattern> <owner>...`;
//   - the LAST matching rule wins;
//   - a pattern containing a slash (other than a trailing one) is anchored to
//     the repo root; a pattern with no slash matches at any depth;
//   - a trailing slash matches a directory and everything under it;
//   - `*` matches within a path segment, `**` matches across segments.
package codeowners

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Rule is one parsed CODEOWNERS line: the original pattern, the precompiled glob
// candidates it expands to, and its owners (verbatim, e.g. "@org/team").
type Rule struct {
	Pattern string
	globs   []string
	Owners  []string
}

// Ruleset is an ordered set of CODEOWNERS rules. Owners() applies last-match-wins.
type Ruleset struct {
	rules []Rule
}

// Parse reads a CODEOWNERS file body into a Ruleset. Malformed lines (no owner)
// are skipped. The result is never nil.
func Parse(content []byte) *Ruleset {
	rs := &Ruleset{}
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue // a pattern with no owner clears ownership in GitHub; we skip
		}
		rs.rules = append(rs.rules, Rule{
			Pattern: fields[0],
			globs:   patternToGlobs(fields[0]),
			Owners:  append([]string(nil), fields[1:]...),
		})
	}
	return rs
}

// Owners returns the owners of the last rule matching path (a workspace-relative,
// slash-separated path), or nil when no rule matches.
func (rs *Ruleset) Owners(path string) []string {
	path = strings.TrimPrefix(path, "/")
	var owners []string
	for _, r := range rs.rules {
		if r.matches(path) {
			owners = r.Owners // last match wins
		}
	}
	return owners
}

// Empty reports whether the ruleset has no rules.
func (rs *Ruleset) Empty() bool { return rs == nil || len(rs.rules) == 0 }

func (r Rule) matches(path string) bool {
	for _, g := range r.globs {
		if ok, _ := doublestar.Match(g, path); ok {
			return true
		}
	}
	return false
}

// patternToGlobs expands a CODEOWNERS pattern into the doublestar globs that
// reproduce its match set. A pattern may name a file or a directory, so an
// unanchored, non-slash pattern (`docs`, `*.go`) yields both the bare match and
// the under-directory match at any depth.
func patternToGlobs(p string) []string {
	dir := strings.HasSuffix(p, "/")
	trimmed := strings.TrimSuffix(p, "/")
	anchored := strings.HasPrefix(p, "/") || strings.Contains(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return nil
	}

	bases := []string{trimmed}
	if !anchored {
		// An unanchored pattern matches at any directory depth.
		bases = append(bases, "**/"+trimmed)
	}

	seen := map[string]bool{}
	var out []string
	add := func(g string) {
		if g != "" && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	for _, b := range bases {
		if dir {
			add(b + "/**")
		} else {
			add(b)       // the path itself (a file, or a dir named exactly)
			add(b + "/**") // or a directory and everything under it
		}
	}
	return out
}
