package catalogresolve

import (
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// authoredToManifest bridges a discover+load+inherit AuthoredManifest
// into the resolved catalogmodel.ComponentManifest shape stage 6+ work
// against. Only fields that travel byte-for-byte from authored shape to
// resolved shape are populated here — inference, deps, hash, and source
// fields are filled by their respective stages later.
//
// Identity fields (`namespace`, `repo`, `componentKey`) are constructed
// from the workspace context: namespace defaults to
// `intent.catalog.namespace` (or `Options.Namespace`, or "default");
// repo from `Options.Repo` (or filepath.Base(WorkspaceRoot)); name
// from `metadata.name`.
//
// `componentKey` is constructed as `<namespace>/<repo>/<name>`. Each
// segment is taken verbatim — segment validation (rule
// `component.name.invalid`) is applied at the validate stage.
func authoredToManifest(am AuthoredManifest, namespace, repo string) *catalogmodel.ComponentManifest {
	c := am.Component
	cm := &catalogmodel.ComponentManifest{
		APIVersion: c.APIVersion,
		Kind:       c.Kind,
		Identity: catalogmodel.ComponentIdentity{
			Name:       c.Metadata.Name,
			Namespace:  namespace,
			Repo:       repo,
			Path:       c.Spec.Path,
			SourceFile: am.SourceFile,
		},
		Metadata: catalogmodel.ComponentMetadata{
			Title:       c.Metadata.Title,
			Description: c.Metadata.Description,
			Owner:       c.Spec.Owner,
			Labels:      copyStrMap(c.Metadata.Labels),
			Annotations: copyStrMap(c.Metadata.Annotations),
		},
		Spec: catalogmodel.ComponentSpec{
			Type:      c.Spec.Type,
			Lifecycle: c.Spec.Lifecycle,
			System:    c.Spec.System,
		},
		Resolution: catalogmodel.ComponentResolution{
			InheritedFrom: provenanceToInheritedFrom(am.Provenance),
			InferredFrom:  map[string][]string{},
		},
	}
	cm.Identity.ComponentKey = catalogmodel.FormatComponentKey(namespace, repo, c.Metadata.Name)

	// Environments — copy authored profile and mark active=true (Phase 2
	// has no environment-active gating yet; the writer can override).
	if len(c.Spec.Environments) > 0 {
		cm.Spec.Environments = make(map[string]catalogmodel.ComponentEnvironment, len(c.Spec.Environments))
		for k, v := range c.Spec.Environments {
			cm.Spec.Environments[k] = catalogmodel.ComponentEnvironment{
				Profile: v.Profile,
				Active:  true,
			}
		}
	}

	return cm
}

// provenanceToInheritedFrom flattens the AuthoredManifest provenance map
// into the ComponentResolution.InheritedFrom shape (field → file). Only
// entries whose Provenance.File differs from the authored manifest's own
// SourceFile (i.e. inherited from intent or composition) are recorded —
// authored fields don't appear in inheritedFrom per
// resolution-pipeline.md §3.
func provenanceToInheritedFrom(prov map[string]Provenance) map[string]string {
	if len(prov) == 0 {
		return map[string]string{}
	}
	// Determine the manifest's own file from any /metadata/* entry — the
	// loader always emits metadata.name with file=manifest.
	var authoredFile string
	if p, ok := prov["metadata.name"]; ok {
		authoredFile = p.File
	}
	out := map[string]string{}
	for field, p := range prov {
		if p.File == "" || p.File == authoredFile {
			continue
		}
		out[field] = p.File
	}
	return out
}

func copyStrMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// resolveNamespace picks the effective namespace for componentKey
// construction per identity-and-keys.md §4: explicit Options.Namespace
// wins, then intent.catalog.namespace, then "default".
func resolveNamespace(opts Options, intent *intentFile) string {
	if opts.Namespace != "" {
		return opts.Namespace
	}
	if intent != nil && intent.Catalog != nil && intent.Catalog.Namespace != "" {
		return intent.Catalog.Namespace
	}
	return "default"
}

// resolveRepo picks the effective repo segment for componentKey
// construction. Falls back to filepath.Base(WorkspaceRoot) when the
// option is unset.
func resolveRepo(opts Options) string {
	if opts.Repo != "" {
		return opts.Repo
	}
	return filepath.Base(opts.WorkspaceRoot)
}
