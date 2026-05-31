package catalogresolve

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// manifestHash computes the per-component manifestHash per
// identity-and-keys.md §10. Inputs are the resolved
// {identity, metadata, spec, runtime} subset of the manifest, encoded
// through catalogmodel.CanonicalEncode (the only allowed input — there
// is no resolver-local encoder).
//
// Provenance (Resolution.InheritedFrom / InferredFrom) is excluded by
// design: changing only provenance must NOT change the hash.
//
// Source.ManifestHash itself is also excluded (it is the field we are
// computing); the hash returns as `sha256:<hex>`.
func manifestHash(cm *catalogmodel.ComponentManifest) (string, error) {
	hashed := struct {
		Identity catalogmodel.ComponentIdentity `json:"identity"`
		Metadata catalogmodel.ComponentMetadata `json:"metadata"`
		Spec     catalogmodel.ComponentSpec     `json:"spec"`
		Runtime  catalogmodel.ComponentRuntime  `json:"runtime"`
	}{
		Identity: cm.Identity,
		Metadata: cm.Metadata,
		Spec:     cm.Spec,
		Runtime:  cm.Runtime,
	}
	canonical, err := catalogmodel.CanonicalEncode(hashed)
	if err != nil {
		return "", &ErrResolverInternal{Stage: 10, Underlying: err}
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
