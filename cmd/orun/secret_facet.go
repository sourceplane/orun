package main

import (
	"github.com/sourceplane/orun/internal/catalogext"
	"github.com/sourceplane/orun/internal/catalogresolve"
	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/sourceplane/orun/internal/loader"
)

// enrichCatalogSecretsFacet derives the static half of the x-orun-secrets facet
// (requirements = the union of every profile's secretBindings across the
// component's composition) and attaches it onto each resolved component manifest
// (orun-secrets SEC4, platform-integration.md §1). Offline-computable, value-free,
// and a no-op for components whose composition declares no secretBindings — so a
// binding-less workspace leaves the catalog unchanged. The live keys
// (bindings/rotation/syncs) are a follow-up (SM4).
func enrichCatalogSecretsFacet(view *catalogresolve.CatalogView, reg *loader.CompositionRegistry) {
	if view == nil || reg == nil || view.ResolvedCatalog == nil {
		return
	}
	for _, m := range view.Manifests {
		if m == nil {
			continue
		}
		composition := compositionForManifestType(reg, m.Spec.Type)
		if composition == nil {
			continue
		}
		reqs := catalogext.DeriveComponentSecretRequirements(composition.ExecutionProfiles)
		m.Extensions = catalogext.AttachSecretsFacet(m.Extensions, reqs)
	}
}

// compositionForManifestType resolves a component's composition type to its
// resolved Composition, preferring the type index and falling back to the key
// index (nil when neither matches).
func compositionForManifestType(reg *loader.CompositionRegistry, compositionType string) *compositionpkg.Composition {
	if compositionType == "" {
		return nil
	}
	if c, ok := reg.Types[compositionType]; ok {
		return c
	}
	if c, ok := reg.ByKey[compositionType]; ok {
		return c
	}
	return nil
}
