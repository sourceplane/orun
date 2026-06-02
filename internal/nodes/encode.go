package nodes

import (
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/objectstore"
)

// Encode returns the canonical JSON bytes for a record. This is the single
// approved path for serializing a node record (the object-model lint gate bans
// json.Marshal of records elsewhere). It is exactly the object store's
// canonical encoder, so a record's bytes — and therefore its object id — are
// deterministic and dedup correctly.
func Encode(v any) ([]byte, error) {
	return objectstore.CanonicalEncode(v)
}

// Decode unmarshals canonical JSON bytes into a record of type T.
func Decode[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("nodes: decode: %w", err)
	}
	return v, nil
}
