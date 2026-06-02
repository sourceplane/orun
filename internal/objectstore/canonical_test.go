package objectstore

import (
	"errors"
	"testing"
)

func TestCanonicalEncodeSortsKeysNoWhitespace(t *testing.T) {
	t.Parallel()
	in := map[string]any{"z": 1, "a": map[string]any{"y": 2, "b": 3}}
	got, err := CanonicalEncode(in)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	want := `{"a":{"b":3,"y":2},"z":1}`
	if string(got) != want {
		t.Fatalf("CanonicalEncode = %s, want %s", got, want)
	}
}

func TestCanonicalEncodeDeterministicAcrossEquivalentInputs(t *testing.T) {
	t.Parallel()
	type rec struct {
		B string `json:"b"`
		A int    `json:"a"`
	}
	fromStruct, err := CanonicalEncode(rec{B: "x", A: 1})
	if err != nil {
		t.Fatalf("encode struct: %v", err)
	}
	fromMap, err := CanonicalEncode(map[string]any{"a": 1, "b": "x"})
	if err != nil {
		t.Fatalf("encode map: %v", err)
	}
	if string(fromStruct) != string(fromMap) {
		t.Fatalf("not deterministic: %s vs %s", fromStruct, fromMap)
	}
}

func TestCanonicalEncodeTypes(t *testing.T) {
	t.Parallel()
	got, err := CanonicalEncode(map[string]any{
		"n": nil, "t": true, "f": false, "s": "hi\n", "arr": []any{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"arr":[1,2,3],"f":false,"n":null,"s":"hi\n","t":true}`
	if string(got) != want {
		t.Fatalf("CanonicalEncode = %s, want %s", got, want)
	}
}

func TestCanonicalEncodeRejectsUnsupported(t *testing.T) {
	t.Parallel()
	if _, err := CanonicalEncode(make(chan int)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for channel, got %v", err)
	}
}
