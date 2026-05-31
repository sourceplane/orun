package catalogmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ManifestHash returns the canonical `sha256:<hex>` digest of the resolved
// portion of a ComponentManifest — `{identity, metadata, spec, runtime}`. Per
// identity-and-keys.md §10:
//
//   - The hash MUST exclude `source.manifestHash` itself (it is set after
//     computation) and `source` in general (which records the SourceSnapshot
//     pointers, not resolved data).
//   - The hash MUST exclude `resolution.inheritedFrom` / `inferredFrom`
//     provenance — changing only provenance MUST NOT change `manifestHash`.
//
// Every input goes through CanonicalEncode so byte ordering is stable across
// map iteration orderings and Go versions. Pure: no side effects.
func ManifestHash(m ComponentManifest) (string, error) {
	payload := struct {
		Identity ComponentIdentity `json:"identity"`
		Metadata ComponentMetadata `json:"metadata"`
		Spec     ComponentSpec     `json:"spec"`
		Runtime  ComponentRuntime  `json:"runtime"`
	}{
		Identity: m.Identity,
		Metadata: m.Metadata,
		Spec:     m.Spec,
		Runtime:  m.Runtime,
	}
	canonical, err := CanonicalEncode(payload)
	if err != nil {
		return "", fmt.Errorf("catalogmodel: manifestHash encode: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CatalogInputHash is the §8 hash over canonical resolver inputs. C0 ships
// the wrapper; the actual input set (intent.yaml + component.yaml fixed
// snapshot + dirty file list) is materialized by C1's sourcectx — for now
// callers pass the pre-assembled input value and we canonicalize + hash it.
func CatalogInputHash(input any) (string, error) {
	canonical, err := CanonicalEncode(input)
	if err != nil {
		return "", fmt.Errorf("catalogmodel: catalogInputHash encode: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
