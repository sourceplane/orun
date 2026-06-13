package work

import (
	"bytes"
	"encoding/json"
	"sort"
)

// Canonical produces a byte-deterministic JSON encoding of v: object keys sorted
// lexicographically at every level, no insignificant whitespace, numbers
// preserved verbatim. It is the comparison form for the invariant-2 proof
// (replaying the log reproduces the projection byte-for-byte) and the basis for
// hashing sealed objects later (W4).
//
// This mirrors internal/catalogmodel.CanonicalEncode but is reimplemented here
// so the work package stays import-isolated from sibling internal/* packages.
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := emitCanonical(&buf, decoded); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CanonicalEqual reports whether a and b have identical canonical encodings.
func CanonicalEqual(a, b any) (bool, error) {
	ab, err := Canonical(a)
	if err != nil {
		return false, err
	}
	bb, err := Canonical(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ab, bb), nil
}

func emitCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
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
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := emitCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := emitCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	}
}
