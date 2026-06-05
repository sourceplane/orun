package catalogresolve

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

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
			Labels:      mergeStrMaps(c.Spec.Labels, c.Metadata.Labels),
			Annotations: copyStrMap(c.Metadata.Annotations),
		},
		Spec: catalogmodel.ComponentSpec{
			Type:       c.Spec.Type,
			Lifecycle:  c.Spec.Lifecycle,
			System:     c.Spec.System,
			Domain:     c.Spec.Domain,
			Parameters: stringifyParams(c.Spec.Parameters),
		},
		Resolution: catalogmodel.ComponentResolution{
			InheritedFrom: provenanceToInheritedFrom(am.Provenance),
			InferredFrom:  map[string][]string{},
		},
	}
	cm.Identity.ComponentKey = catalogmodel.FormatComponentKey(namespace, repo, c.Metadata.Name)

	// Change-detection watches — carry the authored signals into the resolved
	// manifest (the catalog-canonical home the affected engine reads). Pointer +
	// omitempty so a watch-less component leaves the manifest hash unchanged.
	if c.Spec.Change != nil && len(c.Spec.Change.Watches) > 0 {
		cm.Spec.Change = &catalogmodel.ComponentChange{
			Watches: append([]string(nil), c.Spec.Change.Watches...),
		}
	}

	// Environments — copy authored profile and mark active=true (Phase 2
	// has no environment-active gating yet; the writer can override).
	// Both the `environments` map form and the legacy `subscribe`
	// list form fold into the same resolved map. The map form wins on
	// key conflicts so an explicit `environments` entry is never
	// shadowed by a `subscribe` entry for the same env.
	if len(c.Spec.Environments) > 0 || (c.Spec.Subscribe != nil && len(c.Spec.Subscribe.Environments) > 0) {
		cm.Spec.Environments = map[string]catalogmodel.ComponentEnvironment{}
		if c.Spec.Subscribe != nil {
			for _, e := range c.Spec.Subscribe.Environments {
				if e.Name == "" {
					continue
				}
				cm.Spec.Environments[e.Name] = catalogmodel.ComponentEnvironment{
					Profile: e.Profile,
					Active:  true,
				}
			}
		}
		for k, v := range c.Spec.Environments {
			cm.Spec.Environments[k] = catalogmodel.ComponentEnvironment{
				Profile: v.Profile,
				Active:  true,
			}
		}
	}

	return cm
}

// stringifyParams flattens an authored `spec.parameters` map (whose values
// are author-defined YAML scalars) into the resolved manifest's
// map[string]string. Authored values are overwhelmingly strings; non-string
// scalars are rendered deterministically so the manifest hash stays stable.
func stringifyParams(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = stringifyScalar(v)
	}
	return out
}

func stringifyScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		// JSON round-trips all YAML numbers through float64. Emit integers
		// without a trailing ".0" and other values at full precision.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case map[string]any, []any:
		// Non-scalar parameter value (nested map or list). Encode as
		// canonical JSON rather than Go's %v map syntax so the resolved
		// string is stable and machine-readable. json.Marshal sorts map
		// keys, keeping the manifest hash deterministic.
		if b, err := json.Marshal(x); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// mergeStrMaps merges two string maps; keys in `override` win over `base`.
// Returns nil when both are empty so the resolved manifest omits the field.
func mergeStrMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
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
