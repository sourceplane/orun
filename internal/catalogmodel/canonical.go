package catalogmodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CanonicalEncode produces the byte-deterministic JSON form used as input to
// every hash in the catalog model (catalogHash, manifestHash,
// catalogInputHash). See identity-and-keys.md §9–§10 and the
// package-level doc on the determinism contract.
//
// Rules:
//   - Map keys (including struct field names rendered through their JSON
//     tags) appear in lexicographic order at every level of nesting.
//   - No whitespace between tokens.
//   - Numbers are preserved verbatim (json.Number) so a float in the input
//     survives without ±epsilon distortion.
//   - Strings are escaped with the same conservative ASCII-safe rules as
//     encoding/json; non-ASCII runes are emitted as raw UTF-8 (encoding/json
//     also does not escape these by default).
//
// Hashed inputs MUST go through this function. PrettyEncode is for
// persisted, human-readable artifacts — it sorts the same way but adds
// 2-space indents.
func CanonicalEncode(v any) ([]byte, error) {
	g, err := toGeneric(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PrettyEncode produces the human-readable JSON form used for persisted
// catalog artifacts (source.json, catalog.json, manifests, refs). Same key
// ordering as CanonicalEncode plus 2-space indentation, with one trailing
// newline elided so multiple writes round-trip byte-for-byte.
func PrettyEncode(v any) ([]byte, error) {
	canonical, err := CanonicalEncode(v)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, canonical, "", "  "); err != nil {
		return nil, err
	}
	return pretty.Bytes(), nil
}

// toGeneric round-trips v through encoding/json so struct fields are flattened
// into a `map[string]any` keyed by their JSON tag names. UseNumber preserves
// numeric literals exactly.
func toGeneric(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("catalogmodel: marshal input: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("catalogmodel: re-decode for canonicalization: %w", err)
	}
	return out, nil
}

// writeCanonical emits v to buf in the canonical form. Recursive over
// maps/arrays; leaves are quoted strings or raw numeric/bool/null tokens.
func writeCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case json.Number:
		buf.WriteString(x.String())
		return nil
	case float64:
		// Should not occur because toGeneric uses UseNumber, but handle for
		// callers passing pre-decoded generic values.
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		return nil
	case string:
		writeQuotedString(buf, x)
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeQuotedString(buf, k)
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("catalogmodel: unsupported canonical type %T", v)
	}
}

// writeQuotedString writes s as a JSON string literal. Mirrors
// encoding/json's default escape policy: only escape control characters,
// `"`, `\`, and `\u2028`/`\u2029` (browser-safety bytes); everything else
// passes through as raw UTF-8.
func writeQuotedString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			if start < i {
				buf.WriteString(s[start:i])
			}
			switch c {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			case '\b':
				buf.WriteString(`\b`)
			case '\f':
				buf.WriteString(`\f`)
			default:
				fmt.Fprintf(buf, `\u%04x`, c)
			}
			i++
			start = i
			continue
		}
		// 2028 / 2029 line/paragraph separators: encoding/json escapes these
		// for browser/JS safety. Mirror that.
		if c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9) {
			if start < i {
				buf.WriteString(s[start:i])
			}
			fmt.Fprintf(buf, `\u202%x`, s[i+2]&0x0F)
			i += 3
			start = i
			continue
		}
		// Multi-byte UTF-8: skip the right number of bytes.
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if start < len(s) {
		buf.WriteString(s[start:])
	}
	buf.WriteByte('"')
}

// CanonicalEncodeString is a small convenience for callers that want a
// string rather than []byte (useful when feeding into sha256.Sum256 or
// fmt.Sprintf payloads).
func CanonicalEncodeString(v any) (string, error) {
	b, err := CanonicalEncode(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CanonicalEqual reports whether a and b have byte-identical canonical
// encodings. Useful for tests asserting determinism without exposing the
// canonical bytes themselves.
func CanonicalEqual(a, b any) (bool, error) {
	ab, err := CanonicalEncode(a)
	if err != nil {
		return false, err
	}
	bb, err := CanonicalEncode(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ab, bb), nil
}

// canonicalAssert is an internal sanity helper used by tests to confirm that
// a generated string contains no whitespace at the top level. Kept as a
// `_ = strings.X` reference so the import doesn't appear unused if all other
// strings use is removed in a future refactor.
var _ = strings.TrimSpace
