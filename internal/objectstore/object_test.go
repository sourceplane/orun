package objectstore

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseIDValidAndInvalid(t *testing.T) {
	t.Parallel()
	good := ObjectID("sha256:" + strings.Repeat("a", 64))
	if _, _, err := parseID(good); err != nil {
		t.Fatalf("parseID(good): %v", err)
	}
	cases := map[string]ObjectID{
		"empty":         "",
		"no colon":      ObjectID(strings.Repeat("a", 64)),
		"colon at end":  "sha256:",
		"leading colon": ":abc",
		"short hex":     "sha256:abc",
		"long hex":      ObjectID("sha256:" + strings.Repeat("a", 65)),
		"non-hex":       ObjectID("sha256:" + strings.Repeat("g", 64)),
		"unknown algo":  ObjectID("md5:" + strings.Repeat("a", 32)),
	}
	for name, id := range cases {
		if _, _, err := parseID(id); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: parseID(%q) = %v, want ErrInvalid", name, id, err)
		}
	}
}

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()
	body := []byte("hello world")
	kind, got, err := parseFrame(frame(KindBlob, body))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if kind != KindBlob || !bytes.Equal(got, body) {
		t.Fatalf("round-trip mismatch: kind=%s body=%q", kind, got)
	}
}

func TestParseFrameCorruption(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"no type":       []byte("nospacehere"),
		"unknown kind":  []byte("widget 3\x00abc"),
		"no nul":        []byte("blob 3 abc"),
		"bad length":    []byte("blob x\x00abc"),
		"length mismat": []byte("blob 99\x00abc"),
	}
	for name, in := range cases {
		if _, _, err := parseFrame(in); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: parseFrame = %v, want ErrCorrupt", name, err)
		}
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"a", "plan.json", "rev-001", "j_a8f3", "A.B-c_9"} {
		if !validName(ok) {
			t.Fatalf("validName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a b", "a\x00b", "naïve"} {
		if validName(bad) {
			t.Fatalf("validName(%q) = true, want false", bad)
		}
	}
}

func TestTreeBodyRoundTripAndOrder(t *testing.T) {
	t.Parallel()
	algo := AlgoSHA256
	id := func(c string) ObjectID { return ObjectID("sha256:" + strings.Repeat(c, 64)) }
	entries := []TreeEntry{
		{Name: "zeta", Kind: KindBlob, ID: id("2")},
		{Name: "alpha", Kind: KindTree, ID: id("1")},
	}
	sorted, err := sortValidateEntries(entries, algo)
	if err != nil {
		t.Fatalf("sortValidate: %v", err)
	}
	if sorted[0].Name != "alpha" || sorted[1].Name != "zeta" {
		t.Fatalf("entries not sorted: %+v", sorted)
	}
	body := encodeTreeBody(sorted)
	decoded, err := decodeTreeBody(body, algo)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 2 || decoded[0] != sorted[0] || decoded[1] != sorted[1] {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

func TestSortValidateEntriesRejects(t *testing.T) {
	t.Parallel()
	algo := AlgoSHA256
	good := ObjectID("sha256:" + strings.Repeat("a", 64))
	cases := map[string][]TreeEntry{
		"bad name":  {{Name: "a/b", Kind: KindBlob, ID: good}},
		"dup name":  {{Name: "x", Kind: KindBlob, ID: good}, {Name: "x", Kind: KindBlob, ID: good}},
		"bad kind":  {{Name: "x", Kind: Kind("widget"), ID: good}},
		"bad id":    {{Name: "x", Kind: KindBlob, ID: "garbage"}},
		"wrong algo": {{Name: "x", Kind: KindBlob, ID: ObjectID("md5:" + strings.Repeat("a", 32))}},
	}
	for name, entries := range cases {
		if _, err := sortValidateEntries(entries, algo); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// Empty tree is legal.
	if _, err := sortValidateEntries(nil, algo); err != nil {
		t.Fatalf("empty tree: %v", err)
	}
}

func TestDecodeTreeBodyCorruption(t *testing.T) {
	t.Parallel()
	algo := AlgoSHA256
	hex := strings.Repeat("a", 64)
	cases := map[string][]byte{
		"truncated kind": []byte("blob"),
		"bad kind":       append([]byte("widget x\x00"), hex...),
		"truncated id":   []byte("blob x\x00short"),
		"bad name":       append([]byte("blob a/b\x00"), hex...),
	}
	for name, in := range cases {
		if _, err := decodeTreeBody(in, algo); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: decodeTreeBody = %v, want ErrCorrupt", name, err)
		}
	}
	// Not-sorted detection: two valid entries out of order.
	notsorted := append(append([]byte("blob zeta\x00"), hex...), append([]byte("blob alpha\x00"), hex...)...)
	if _, err := decodeTreeBody(notsorted, algo); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("not-sorted: %v, want ErrCorrupt", err)
	}
}
