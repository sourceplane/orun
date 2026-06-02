package objectstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalEncode produces the byte-deterministic JSON form that every record
// blob is hashed and stored as (object-store.md §3). Two values that are
// semantically equal produce byte-identical output, which is what makes content
// addressing dedup correctly.
//
// Rules:
//   - Object keys appear in lexicographic (byte) order at every nesting level.
//   - No insignificant whitespace.
//   - Numbers are preserved verbatim via json.Number (no float ±epsilon drift).
//   - Strings are escaped with encoding/json's rules.
//
// Record blobs MUST be encoded through this function; ad-hoc json.Marshal of a
// record is banned by the object-model lint gate (claude-goals.md §3). The
// nodes package (M3) re-exports this as its canonical encoder.
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

// toGeneric round-trips v through encoding/json so struct fields collapse into
// map[string]any keyed by their JSON tags, with UseNumber preserving numeric
// precision.
func toGeneric(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrInvalid, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var g any
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalid, err)
	}
	return g, nil
}

// writeCanonical emits g in canonical form. It handles the generic shapes that
// survive a json round-trip: map[string]any, []any, json.Number, string, bool,
// and nil.
func writeCanonical(buf *bytes.Buffer, g any) error {
	switch t := g.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(t.String())
	case string:
		// Delegate string escaping to encoding/json for correctness.
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("%w: marshal string: %v", ErrInvalid, err)
		}
		buf.Write(b)
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return fmt.Errorf("%w: marshal key: %v", ErrInvalid, err)
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonical(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("%w: uncanonicalizable type %T", ErrInvalid, g)
	}
	return nil
}
