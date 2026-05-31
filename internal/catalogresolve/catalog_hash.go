package catalogresolve

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// catalogHash computes the catalog-level hash per identity-and-keys.md §9.
//
// Inputs, in this order:
//
//  1. catalogInputHash (caller-supplied, from sourcectx.CatalogInputHash)
//  2. The list of (componentKey, manifestHash) pairs, ordered by
//     componentKey ascending.
//  3. The canonical encoding of every CatalogGraph, in the fixed order
//     [dependencies, systems, apis, resources, owners].
//  4. resolverVersion (caller-supplied, from ResolverInputs).
//
// The hashed payload is itself canonical-encoded so the wire form is
// stable across runs. Output is `sha256:<hex>`.
//
// Implementation note: graphs are emitted as their full struct shapes —
// the catalogSnapshotKey field is intentionally cleared before encoding
// because the key is derived FROM catalogHash and would otherwise create
// a circular dependency (key changes ⇒ hash changes ⇒ key changes…).
func catalogHash(catalogInputHash string, manifests []*catalogmodel.ComponentManifest, graphs []*catalogmodel.CatalogGraph, resolverVersion int) (string, error) {
	pairs := make([]catalogHashPair, 0, len(manifests))
	for _, m := range manifests {
		pairs = append(pairs, catalogHashPair{
			ComponentKey: m.Identity.ComponentKey,
			ManifestHash: m.Source.ManifestHash,
		})
	}
	// manifests are already sorted by ComponentKey at the Resolve layer,
	// but the canonical encoder also sorts map keys; the slice order here
	// is what matters. We trust the caller's ordering; T-IDK-1 covers it.

	graphPayloads := make([]any, 0, len(graphs))
	for _, g := range graphs {
		// Strip the catalogSnapshotKey field for hashing — it is derived
		// from this hash and would create a self-reference. Source key
		// stays (it's an input from sourcectx, not derived from us).
		clone := *g
		clone.CatalogSnapshotKey = ""
		graphPayloads = append(graphPayloads, &clone)
	}

	payload := struct {
		CatalogInputHash string            `json:"catalogInputHash"`
		Manifests        []catalogHashPair `json:"manifests"`
		Graphs           []any             `json:"graphs"`
		ResolverVersion  int               `json:"resolverVersion"`
	}{
		CatalogInputHash: catalogInputHash,
		Manifests:        pairs,
		Graphs:           graphPayloads,
		ResolverVersion:  resolverVersion,
	}

	canonical, err := catalogmodel.CanonicalEncode(payload)
	if err != nil {
		return "", &ErrResolverInternal{Stage: 12, Underlying: err}
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// catalogHashPair is the (componentKey, manifestHash) pair that feeds
// the catalogHash input list.
type catalogHashPair struct {
	ComponentKey string `json:"componentKey"`
	ManifestHash string `json:"manifestHash"`
}
