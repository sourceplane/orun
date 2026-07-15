package scaffold

import (
	"fmt"
	"strings"
	"text/template"
)

// constrainedFuncMap is the CLOSED allow-list of template helpers (design §7.2).
// Only pure string/data-shaping helpers are exposed. It MUST NOT expose any
// function that reads the filesystem, executes a process, opens a socket, reads
// env, or returns nondeterministic data (time/random/UUID). Any addition is a
// reviewed change, not an open extension point.
//
// The denylist is also structural: no os/exec/net/io/time/rand appears in this
// package's import set (enforced by neutrality_test.go / imports_test.go).
func constrainedFuncMap() template.FuncMap {
	return template.FuncMap{
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"title": func(s string) string { return strings.Title(strings.ToLower(s)) }, //nolint:staticcheck // ASCII title is intentional and deterministic
		"trim":  strings.TrimSpace,
		"kebab": kebab,
		"slug":  kebab, // slug is an alias for DNS-safe kebab
		"quote": func(s string) string { return fmt.Sprintf("%q", s) },
		"default": func(def, val any) any {
			if isEmpty(val) {
				return def
			}
			return val
		},
		"indent": indent,
	}
}

// kebab lowercases and replaces any run of non-[a-z0-9] characters with a
// single hyphen, trimming leading/trailing hyphens — a DNS-safe slug.
func kebab(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// indent prefixes every line of s with n spaces (used to nest a rendered block
// inside YAML). Deterministic, pure string shaping.
func indent(n int, s string) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func isEmpty(val any) bool {
	switch v := val.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case int:
		return v == 0
	case float64:
		return v == 0
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}
