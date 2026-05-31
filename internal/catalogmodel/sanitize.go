package catalogmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Sanitizers are pure, total functions used to derive filesystem-safe
// segments from arbitrary user input. None of them panic on any input —
// guaranteed by property test T-IDK-5.
//
// See identity-and-keys.md §12.

const (
	// branchSanitizedMax is the maximum allowed length of a sanitized branch
	// segment before the truncate-and-hash rule kicks in (identity-and-keys.md
	// §2 rule 2: "Max 40 chars; longer names truncated and suffixed with
	// first 8 chars of sha256(branch)").
	branchSanitizedMax = 40
	// branchHashSuffixLen is the number of hex chars from sha256(branch)
	// appended after the truncation point.
	branchHashSuffixLen = 8
)

// SanitizeBranch turns an arbitrary git branch name into a key segment that
// matches `[a-z0-9-]{1,40}` (or `<truncated>-<8-hex>` when the input would
// otherwise exceed 40 sanitized characters).
//
// Rules (identity-and-keys.md §2 rule 2):
//   - lowercase
//   - any character outside [a-z0-9-] becomes '-'
//   - runs of '-' are collapsed
//   - leading/trailing '-' are trimmed
//   - if the sanitized form exceeds 40 chars, truncate to 31 chars + '-' +
//     first 8 hex chars of sha256(original)
//
// Total: any input string returns a valid segment. Empty input returns "".
func SanitizeBranch(name string) string {
	clean := lowerAndDashOnly(name)
	clean = collapseDashes(clean)
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return ""
	}
	if len(clean) <= branchSanitizedMax {
		return clean
	}
	// Truncation path: keep first (40 - 1 - 8) = 31 chars, separator '-', then
	// 8 hex of sha256 over the ORIGINAL input so distinct branches that share
	// a sanitized prefix don't collide.
	keep := branchSanitizedMax - 1 - branchHashSuffixLen
	prefix := clean[:keep]
	prefix = strings.TrimRight(prefix, "-")
	sum := sha256.Sum256([]byte(name))
	return prefix + "-" + hex.EncodeToString(sum[:])[:branchHashSuffixLen]
}

// SanitizeComponentKey turns a 3-segment componentKey
// (`<namespace>/<repo>/<name>`) into the filename form used under
// indexes/components/. Slashes become single dashes; no other transformation
// is applied because each segment is already required to match
// `[a-z0-9._-]+`.
//
// Total: any input string returns a string that contains no '/' characters.
func SanitizeComponentKey(componentKey string) string {
	if componentKey == "" {
		return ""
	}
	return strings.ReplaceAll(componentKey, "/", "-")
}

// SanitizeEventKind turns a dotted event kind ("execution.completed") into
// the segment used in event filenames ("execution-completed"). Dots become
// dashes; no other transformation.
//
// Total: any input string returns a string that contains no '.' characters.
func SanitizeEventKind(kind string) string {
	if kind == "" {
		return ""
	}
	return strings.ReplaceAll(kind, ".", "-")
}

// ShortHex returns the first n hex characters of a hex-encoded string,
// lowercased. Returns the empty string when `full` is shorter than n or
// contains any non-hex byte. Total — never panics.
func ShortHex(full string, n int) string {
	if n <= 0 {
		return ""
	}
	full = strings.ToLower(strings.TrimSpace(full))
	if len(full) < n {
		return ""
	}
	for i := 0; i < n; i++ {
		c := full[i]
		if !isHex(c) {
			return ""
		}
	}
	return full[:n]
}

// lowerAndDashOnly is the lowercase + non-[a-z0-9-] -> '-' transform used by
// SanitizeBranch. Walks bytes (input is treated as UTF-8 but only ASCII
// alnum survives, so the byte loop is safe — multi-byte runes get mapped to
// '-' just like any other disallowed byte).
func lowerAndDashOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-':
			b.WriteByte('-')
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// collapseDashes replaces runs of '-' with a single '-'. Loops until stable.
func collapseDashes(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// isHex returns true if c is one of [0-9a-f]. Inputs are expected to be
// pre-lowercased by the caller (ShortHex calls strings.ToLower first).
func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
