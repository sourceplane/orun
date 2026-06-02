package objectstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Algo names a content-hash algorithm. The algorithm is a first-class value so
// a future blake3 is a config swap, not a migration crisis (identity-and-keys.md
// §2). Objects are namespaced by algo on disk (objects/<algo>/…) and ids carry
// the algo verbatim ("<algo>:<hex>").
type Algo string

const (
	// AlgoSHA256 is the v1 default. Boring, proven, already emitted as
	// sha256-<hex> plan checksums elsewhere in orun.
	AlgoSHA256 Algo = "sha256"
)

// DefaultAlgo is the algorithm new stores use unless configured otherwise.
const DefaultAlgo = AlgoSHA256

// hexLen returns the fixed hex-string width an algo's digest occupies. Tree
// encoding relies on this being constant so entries are parseable without a
// separator after the id (object-store.md §2.1).
func (a Algo) hexLen() (int, error) {
	switch a {
	case AlgoSHA256:
		return sha256.Size * 2, nil
	default:
		return 0, fmt.Errorf("%w: unknown hash algo %q", ErrInvalid, a)
	}
}

// sum returns the lowercase-hex digest of data under the algo.
func (a Algo) sum(data []byte) (string, error) {
	switch a {
	case AlgoSHA256:
		h := sha256.Sum256(data)
		return hex.EncodeToString(h[:]), nil
	default:
		return "", fmt.Errorf("%w: unknown hash algo %q", ErrInvalid, a)
	}
}

// idFor returns the wire-form ObjectID ("<algo>:<hex>") for already-serialized
// (framed) object bytes.
func (a Algo) idFor(serialized []byte) (ObjectID, error) {
	hexsum, err := a.sum(serialized)
	if err != nil {
		return "", err
	}
	return ObjectID(string(a) + ":" + hexsum), nil
}

// ValidateID reports whether id is a well-formed "<algo>:<hex>" object id under
// a known algorithm, returning ErrInvalid otherwise. Exported for sibling
// packages (e.g. refstore) that accept ids as targets.
func ValidateID(id ObjectID) error {
	_, _, err := parseID(id)
	return err
}

// parseID splits an ObjectID into its algo and hex parts, validating the shape
// and the hex width/alphabet against the algo. It returns ErrInvalid on any
// malformation so callers can route bad ids uniformly.
func parseID(id ObjectID) (Algo, string, error) {
	s := string(id)
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return "", "", fmt.Errorf("%w: object id %q is not \"<algo>:<hex>\"", ErrInvalid, id)
	}
	algo := Algo(s[:i])
	hexpart := s[i+1:]
	wantLen, err := algo.hexLen()
	if err != nil {
		return "", "", err
	}
	if len(hexpart) != wantLen {
		return "", "", fmt.Errorf("%w: object id %q has %d hex chars, want %d for %s", ErrInvalid, id, len(hexpart), wantLen, algo)
	}
	for j := 0; j < len(hexpart); j++ {
		c := hexpart[j]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", "", fmt.Errorf("%w: object id %q has non-lowerhex char %q", ErrInvalid, id, c)
		}
	}
	return algo, hexpart, nil
}
