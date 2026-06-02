package objectstore

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

// ObjectID is the wire-form content address of an object: "<algo>:<hex>"
// (e.g. "sha256:9f86d0…"). It is computed over an object's framed canonical
// serialization (§ serialize) and is identical on every machine for identical
// content — the property that makes dedup and remote substitution work.
type ObjectID string

// Kind is the structural object kind. There are exactly two; typed nodes
// (Source, Catalog, Revision, …) are blobs or trees, not a third kind
// (object-store.md §1).
type Kind string

const (
	// KindBlob is an opaque, ordered byte string (a record body or a raw artifact).
	KindBlob Kind = "blob"
	// KindTree is a sorted set of named entries pointing at child objects.
	KindTree Kind = "tree"
)

// TreeEntry is one child reference inside a tree.
type TreeEntry struct {
	// Name is the entry's filename within the tree. Matches ^[A-Za-z0-9._-]+$,
	// is not "." or "..", and is unique within a tree.
	Name string
	// Kind is the referenced object's structural kind.
	Kind Kind
	// ID is the referenced object's content id.
	ID ObjectID
}

// validName reports whether name is a legal tree-entry / ref path segment:
// non-empty, not "." or "..", and only [A-Za-z0-9._-].
func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// frame wraps a body in the git-style header "<type> <len>\x00" + body. Framing
// the type and length prevents cross-kind type confusion in the hash
// (object-store.md §2).
func frame(kind Kind, body []byte) []byte {
	header := string(kind) + " " + strconv.Itoa(len(body)) + "\x00"
	out := make([]byte, 0, len(header)+len(body))
	out = append(out, header...)
	out = append(out, body...)
	return out
}

// parseFrame splits a framed serialization into its kind and body, validating
// the declared length. Returns ErrInvalid / ErrCorrupt on malformation.
func parseFrame(serialized []byte) (Kind, []byte, error) {
	sp := bytes.IndexByte(serialized, ' ')
	if sp <= 0 {
		return "", nil, fmt.Errorf("%w: object frame missing type", ErrCorrupt)
	}
	kind := Kind(serialized[:sp])
	if kind != KindBlob && kind != KindTree {
		return "", nil, fmt.Errorf("%w: object frame unknown kind %q", ErrCorrupt, kind)
	}
	rest := serialized[sp+1:]
	nul := bytes.IndexByte(rest, 0)
	if nul < 0 {
		return "", nil, fmt.Errorf("%w: object frame missing NUL", ErrCorrupt)
	}
	declared, err := strconv.Atoi(string(rest[:nul]))
	if err != nil || declared < 0 {
		return "", nil, fmt.Errorf("%w: object frame bad length", ErrCorrupt)
	}
	body := rest[nul+1:]
	if len(body) != declared {
		return "", nil, fmt.Errorf("%w: object frame length %d != body %d", ErrCorrupt, declared, len(body))
	}
	return kind, body, nil
}

// sortValidateEntries returns a copy of entries sorted by name, rejecting empty
// input only at the caller's discretion (an empty tree is legal). It enforces
// the name alphabet, name uniqueness, valid kinds, and well-formed ids under
// algo.
func sortValidateEntries(entries []TreeEntry, algo Algo) ([]TreeEntry, error) {
	out := make([]TreeEntry, len(entries))
	copy(out, entries)
	seen := make(map[string]struct{}, len(out))
	for _, e := range out {
		if !validName(e.Name) {
			return nil, fmt.Errorf("%w: tree entry name %q", ErrInvalid, e.Name)
		}
		if _, dup := seen[e.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate tree entry name %q", ErrInvalid, e.Name)
		}
		seen[e.Name] = struct{}{}
		if e.Kind != KindBlob && e.Kind != KindTree {
			return nil, fmt.Errorf("%w: tree entry %q has kind %q", ErrInvalid, e.Name, e.Kind)
		}
		eAlgo, _, err := parseID(e.ID)
		if err != nil {
			return nil, fmt.Errorf("tree entry %q: %w", e.Name, err)
		}
		if eAlgo != algo {
			return nil, fmt.Errorf("%w: tree entry %q id algo %q != store algo %q", ErrInvalid, e.Name, eAlgo, algo)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// encodeTreeBody renders sorted, validated entries as the canonical tree body:
// for each entry, "<kind> <name>\x00<hex>" with hex being the fixed-width digest
// (no "algo:" prefix). The fixed hex width lets decodeTreeBody read entries
// without a trailing separator (object-store.md §2.1).
func encodeTreeBody(sorted []TreeEntry) []byte {
	var buf bytes.Buffer
	for _, e := range sorted {
		buf.WriteString(string(e.Kind))
		buf.WriteByte(' ')
		buf.WriteString(e.Name)
		buf.WriteByte(0)
		// e.ID validated as "<algo>:<hex>"; write the hex tail only.
		hexpart := string(e.ID)[len(e.ID)-hexTail(e.ID):]
		buf.WriteString(hexpart)
	}
	return buf.Bytes()
}

// hexTail returns the length of the hex portion of an already-validated id.
func hexTail(id ObjectID) int {
	s := string(id)
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return len(s) - i - 1
		}
	}
	return 0
}

// decodeTreeBody parses a canonical tree body into entries, reconstructing each
// id as "<algo>:<hex>" using the store's algo. It enforces sort order and name
// validity so a hand-corrupted tree is rejected.
func decodeTreeBody(body []byte, algo Algo) ([]TreeEntry, error) {
	hexLen, err := algo.hexLen()
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	var prevName string
	i := 0
	for i < len(body) {
		// kind up to SP
		sp := bytes.IndexByte(body[i:], ' ')
		if sp < 0 {
			return nil, fmt.Errorf("%w: tree body truncated at kind", ErrCorrupt)
		}
		kind := Kind(body[i : i+sp])
		if kind != KindBlob && kind != KindTree {
			return nil, fmt.Errorf("%w: tree body bad kind %q", ErrCorrupt, kind)
		}
		i += sp + 1
		// name up to NUL
		nul := bytes.IndexByte(body[i:], 0)
		if nul < 0 {
			return nil, fmt.Errorf("%w: tree body truncated at name", ErrCorrupt)
		}
		name := string(body[i : i+nul])
		if !validName(name) {
			return nil, fmt.Errorf("%w: tree body bad name %q", ErrCorrupt, name)
		}
		if prevName != "" && name <= prevName {
			return nil, fmt.Errorf("%w: tree body not sorted (%q after %q)", ErrCorrupt, name, prevName)
		}
		prevName = name
		i += nul + 1
		// fixed-width hex
		if i+hexLen > len(body) {
			return nil, fmt.Errorf("%w: tree body truncated at id", ErrCorrupt)
		}
		hexpart := string(body[i : i+hexLen])
		i += hexLen
		entries = append(entries, TreeEntry{
			Name: name,
			Kind: kind,
			ID:   ObjectID(string(algo) + ":" + hexpart),
		})
	}
	return entries, nil
}
