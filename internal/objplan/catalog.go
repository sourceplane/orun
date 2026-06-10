package objplan

import (
	"encoding/json"
	"path"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
)

// ownershipSchemaVersion is the on-disk ImpactOwnership schema this build emits.
// Bump on any shape change (data-model.md §2).
const ownershipSchemaVersion = 1

// catalogGlobalBlocks are the intent.yaml blocks a change-detection consumer
// treats as catalog-relevant (data-model.md §2). Fixed for schemaVersion 1.
var catalogGlobalBlocks = []string{
	"catalog.defaults",
	"catalog.inference",
	"catalog.discovery",
	"metadata.namespace",
	"metadata.repo",
}

// structuralFilenames are the manifest basenames whose add/remove/edit is a
// structural change (mirrors catalogresolve discovery).
var structuralFilenames = []string{"component.yaml", "component.yml"}

// graphKinds is the canonical edge-kind order the resolver emits graphs in
// (catalog_hash.go: [dependencies, systems, apis, resources, owners]). The
// catalogmodel.CatalogGraph type carries no edge-kind field — it is positional —
// so the adapter assigns kinds by index.
var graphKinds = []string{"dependencies", "systems", "apis", "resources", "owners"}

// BuildCatalogNodes maps a resolved catalogresolve.CatalogView into the
// object-model node types: a CatalogSnapshot, its ComponentManifests, its
// CatalogGraphs, and the change-detection ImpactOwnership map. resolverVersion
// stamps the snapshot (it participates in the resolve memo key, not in catalog
// identity).
func BuildCatalogNodes(view *catalogresolve.CatalogView, resolverVersion int, ownerResolver OwnerResolver) (nodes.CatalogSnapshot, []nodes.ComponentManifest, []nodes.CatalogGraph, nodes.ImpactOwnership, []nodes.ComponentFingerprint) {
	cat := nodes.CatalogSnapshot{
		Kind:            nodes.KindCatalogSnapshot,
		ResolverVersion: resolverVersion,
	}
	if view != nil && view.Snapshot != nil {
		cat.HumanKey = view.Snapshot.CatalogSnapshotKey
	}

	var manifests []nodes.ComponentManifest
	if view != nil {
		for _, cm := range view.Manifests {
			if cm == nil {
				continue
			}
			manifests = append(manifests, mapEntity(cm, resolverVersion, ownerResolver))
		}
	}

	var graphs []nodes.CatalogGraph
	if view != nil {
		for i, g := range view.Graphs {
			if g == nil {
				continue
			}
			edgeKind := "graph" + itoa(i)
			if i < len(graphKinds) {
				edgeKind = graphKinds[i]
			}
			graphs = append(graphs, mapGraph(g, edgeKind))
		}
	}
	return cat, manifests, graphs, buildOwnership(view), buildFingerprints(view)
}

// buildFingerprints maps the resolver's neutral fingerprint set onto the node
// type. Kind/SchemaVersion are defaulted at assembly time.
func buildFingerprints(view *catalogresolve.CatalogView) []nodes.ComponentFingerprint {
	if view == nil || view.ResolvedCatalog == nil {
		return nil
	}
	out := make([]nodes.ComponentFingerprint, 0, len(view.Fingerprints))
	for _, fp := range view.Fingerprints {
		out = append(out, nodes.ComponentFingerprint{
			Kind:          nodes.KindComponentFingerprint,
			SchemaVersion: ownershipSchemaVersion,
			ComponentKey:  fp.ComponentKey,
			Dir:           fp.Dir,
			Subtree:       fp.Subtree,
			Files:         fp.Files,
			GlobalDigest:  fp.GlobalDigest,
		})
	}
	return out
}

// buildOwnership derives the change-detection ownership map from the resolved
// view: each component's directory (the dirname of its component.yaml) maps to
// its componentKey, plus the fixed classification rules. Deterministic — no
// timestamps, sorted arrays — so it folds stably into the catalog Merkle root.
func buildOwnership(view *catalogresolve.CatalogView) nodes.ImpactOwnership {
	o := nodes.ImpactOwnership{
		Kind:                nodes.KindImpactOwnership,
		SchemaVersion:       ownershipSchemaVersion,
		GlobalBlocks:        append([]string(nil), catalogGlobalBlocks...),
		StructuralFilenames: append([]string(nil), structuralFilenames...),
	}

	intentPath := "intent.yaml"
	var excludes []string
	if view != nil && view.ResolvedCatalog != nil {
		if view.IntentPath != "" {
			intentPath = view.IntentPath
		}
		excludes = view.Excludes
	}
	o.GlobalPaths = []string{intentPath}

	o.IgnoreDirs = append([]string(nil), excludes...)
	if o.IgnoreDirs == nil {
		o.IgnoreDirs = catalogresolve.DefaultExcludes()
	}
	sort.Strings(o.IgnoreDirs)

	components := map[string]string{}
	if view != nil && view.ResolvedCatalog != nil {
		for _, cm := range view.Manifests {
			if cm == nil || cm.Identity.SourceFile == "" {
				continue // synthetic root / no authoring file: no ownership entry
			}
			// The component dir is the dirname of its component.yaml location
			// (SourceFile); spec.path is authored-optional and usually absent.
			dir := path.Dir(cm.Identity.SourceFile)
			components[dir] = cm.Identity.ComponentKey
		}
	}
	if len(components) > 0 {
		o.Components = components
	}
	return o
}

// mapEntity reshapes a catalogmodel.ComponentManifest into the Component-kind
// entity envelope node (orun-service-catalog/data-model.md §2). The flat
// authored metadata splits into metadata/ownership/lifecycle; dependencies and
// system/domain/owner edges promote to relations; provided/consumed APIs become
// contracts; inference/inheritance stays in provenance. The deep spec/runtime
// blocks are carried verbatim so no resolved information is lost.
func mapEntity(cm *catalogmodel.ComponentManifest, resolverVersion int, ownerResolver OwnerResolver) nodes.ComponentManifest {
	own := resolveOwner(cm, ownerResolver)
	m := nodes.ComponentManifest{
		APIVersion: catalogmodel.APIVersionV1,
		Kind:       nodes.KindComponentManifest,
		Identity: nodes.ComponentIdentity{
			ComponentKey: cm.Identity.ComponentKey,
			Name:         cm.Identity.Name,
			Namespace:    cm.Identity.Namespace,
			Repo:         cm.Identity.Repo,
			// Node path is the component.yaml *location* (data-model §4) — the
			// resolver's SourceFile, not the authored-optional spec.path. Its
			// dirname is the component dir for ownership/fingerprints.
			Path: cm.Identity.SourceFile,
		},
		Type:         cm.Spec.Type,
		Metadata:     entityMetadata(cm.Metadata),
		Ownership:    entityOwnership(cm.Metadata, own),
		Lifecycle:    entityLifecycle(cm.Spec),
		Spec:         toMap(cm.Spec),
		Relations:    entityRelations(cm, own),
		Contracts:    entityContracts(cm.Spec.Dependencies.APIs),
		Integrations: cm.Integrations,
		Docs:         docsBlock(cm.Docs),
		Links:        linksBlock(cm.Links),
		Extensions:   cm.Extensions,
		Provenance:   entityProvenance(cm.Resolution, resolverVersion),
	}
	return m
}

// docsBlock projects the docs pointers onto the envelope's generic docs map.
func docsBlock(d *catalogmodel.ComponentDocs) map[string]any {
	if d == nil {
		return nil
	}
	out := map[string]any{}
	putNonEmpty(out, "techdocs", d.TechDocs)
	if len(d.Runbooks) > 0 {
		out["runbooks"] = strSliceToAny(d.Runbooks)
	}
	if len(d.ADRs) > 0 {
		out["adrs"] = strSliceToAny(d.ADRs)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// linksBlock projects the external links onto the envelope's generic link list.
func linksBlock(links []catalogmodel.ComponentLink) []map[string]any {
	if len(links) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		m := map[string]any{"title": l.Title, "url": l.URL}
		putNonEmpty(m, "icon", l.Icon)
		out = append(out, m)
	}
	return out
}

// resolvedOwner is the effective ownership claim for an entity: the primary
// owner, any additional owners, and the source of the claim (S-2).
type resolvedOwner struct {
	owner      string
	additional []string
	source     string // authored | CODEOWNERS | unknown
}

// resolveOwner applies the ownership precedence (design.md §4.3, S-2): an
// authored metadata.owner wins (source=authored); otherwise CODEOWNERS over the
// component's source path supplies it (source=CODEOWNERS); otherwise the entity
// is unowned (source=unknown) — flagged, never silently false-owned.
func resolveOwner(cm *catalogmodel.ComponentManifest, ownerResolver OwnerResolver) resolvedOwner {
	if cm.Metadata.Owner != "" {
		return resolvedOwner{
			owner:      cm.Metadata.Owner,
			additional: append([]string(nil), cm.Metadata.Maintainers...),
			source:     catalogmodel.OwnershipSourceAuthored,
		}
	}
	if ownerResolver != nil {
		if owners := ownerResolver(cm.Identity.SourceFile); len(owners) > 0 {
			return resolvedOwner{
				owner:      owners[0],
				additional: append(append([]string(nil), owners[1:]...), cm.Metadata.Maintainers...),
				source:     catalogmodel.OwnershipSourceCODEOWNERS,
			}
		}
	}
	return resolvedOwner{owner: catalogmodel.OwnershipSourceUnknown, source: catalogmodel.OwnershipSourceUnknown}
}

// toMap round-trips a value through JSON into a generic map so the node record
// carries the resolver's nested data canonically (the spec block is kept whole
// and lossless — dependencies/system/domain stay here through SC1; SC2 promotes
// them fully to relations.json). Returns nil for an empty result.
func toMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// entityMetadata projects the descriptive block (owner/maintainers/contacts
// move to ownership; lifecycle/tier move to lifecycle).
func entityMetadata(md catalogmodel.ComponentMetadata) map[string]any {
	out := map[string]any{}
	putNonEmpty(out, "title", md.Title)
	putNonEmpty(out, "description", md.Description)
	if len(md.Labels) > 0 {
		out["labels"] = strMapToAny(md.Labels)
	}
	if len(md.Tags) > 0 {
		out["tags"] = strSliceToAny(md.Tags)
	}
	if len(md.Annotations) > 0 {
		out["annotations"] = strMapToAny(md.Annotations)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// entityOwnership builds the ownership block from the resolved owner (authored
// or CODEOWNERS-derived), recording the claim source (S-2). The block is always
// present (owner may be "unknown") so the portal can render an ownership cell
// for every entity.
func entityOwnership(md catalogmodel.ComponentMetadata, own resolvedOwner) map[string]any {
	out := map[string]any{
		"owner":  own.owner,
		"source": own.source,
	}
	if len(own.additional) > 0 {
		out["additionalOwners"] = strSliceToAny(own.additional)
	}
	if len(md.Contacts) > 0 {
		// Deterministic order: sort by contact type.
		types := make([]string, 0, len(md.Contacts))
		for t := range md.Contacts {
			types = append(types, t)
		}
		sort.Strings(types)
		contacts := make([]any, 0, len(types))
		for _, t := range types {
			contacts = append(contacts, map[string]any{"type": t, "value": md.Contacts[t]})
		}
		out["contacts"] = contacts
	}
	return out
}

// entityLifecycle builds the lifecycle block. `stage` defaults to experimental
// (data-model.md §2); `maturity` is emitted as explicit null — it is recomputed
// from L2 by the v2 scorecard engine and never authored (CR-1).
func entityLifecycle(spec catalogmodel.ComponentSpec) map[string]any {
	stage := spec.Lifecycle
	if stage == "" {
		stage = catalogmodel.LifecycleStageExperimental
	}
	out := map[string]any{
		"stage":    stage,
		"maturity": nil,
	}
	putNonEmpty(out, "tier", spec.Tier)
	return out
}

// entityRelations promotes the resolved dependency/membership/ownership edges
// into the envelope's typed relations array (data-model.md §3): ownedBy (owner),
// partOf (system/domain), dependsOn (component + resource edges). Sorted by
// (type, to) for determinism.
func entityRelations(cm *catalogmodel.ComponentManifest, own resolvedOwner) []nodes.EntityRelation {
	var rels []nodes.EntityRelation
	if own.source != catalogmodel.OwnershipSourceUnknown && own.owner != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeOwnedBy, To: own.owner, ToKind: catalogmodel.EntityKindGroup,
		})
	}
	if cm.Spec.System != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypePartOf, To: cm.Spec.System, ToKind: catalogmodel.EntityKindSystem,
		})
	}
	if cm.Spec.Domain != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypePartOf, To: cm.Spec.Domain, ToKind: catalogmodel.EntityKindDomain,
		})
	}
	for _, d := range cm.Spec.Dependencies.Components {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeDependsOn, To: d.Key, ToKind: catalogmodel.EntityKindComponent,
			Optional: d.Optional, Include: d.Include,
		})
	}
	for _, r := range cm.Spec.Dependencies.Resources.Uses {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeDependsOn, To: r, ToKind: catalogmodel.EntityKindResource,
		})
	}
	// deployedTo edges from the component's environment bindings (SC4): each
	// declared environment becomes a derived Environment entity that the
	// component deploys to. Sorted by env name for determinism.
	envNames := make([]string, 0, len(cm.Spec.Environments))
	for env := range cm.Spec.Environments {
		envNames = append(envNames, env)
	}
	sort.Strings(envNames)
	for _, env := range envNames {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeDeployedTo, To: env, ToKind: catalogmodel.EntityKindEnvironment,
		})
	}
	sort.SliceStable(rels, func(i, j int) bool {
		if rels[i].Type != rels[j].Type {
			return rels[i].Type < rels[j].Type
		}
		return rels[i].To < rels[j].To
	})
	return rels
}

// entityContracts builds the contracts block from the resolved provided/consumed
// APIs (data-model.md §2). Returns nil when no APIs are declared.
func entityContracts(apis catalogmodel.APIDependencies) map[string]any {
	out := map[string]any{}
	if len(apis.Provides) > 0 {
		provides := make([]any, 0, len(apis.Provides))
		for _, a := range apis.Provides {
			provides = append(provides, map[string]any{"api": a})
		}
		out["provides"] = provides
	}
	if len(apis.Consumes) > 0 {
		consumes := make([]any, 0, len(apis.Consumes))
		for _, a := range apis.Consumes {
			consumes = append(consumes, map[string]any{"api": a})
		}
		out["consumes"] = consumes
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// entityProvenance carries the inheritance/inference trail plus the resolver
// stamp. Deterministic from inputs, so it folds stably into the catalog id.
func entityProvenance(res catalogmodel.ComponentResolution, resolverVersion int) map[string]any {
	out := map[string]any{
		"resolver": map[string]any{
			"resolverVersion": resolverVersion,
			"schemaVersion":   catalogmodel.APIVersionV1,
		},
	}
	if len(res.InheritedFrom) > 0 {
		out["inheritedFrom"] = strMapToAny(res.InheritedFrom)
	}
	if len(res.InferredFrom) > 0 {
		inf := map[string]any{}
		for k, v := range res.InferredFrom {
			inf[k] = strSliceToAny(v)
		}
		out["inferredFrom"] = inf
	}
	return out
}

func putNonEmpty(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func strMapToAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func strSliceToAny(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func mapGraph(g *catalogmodel.CatalogGraph, edgeKind string) nodes.CatalogGraph {
	out := nodes.CatalogGraph{Kind: nodes.KindCatalogGraph, EdgeKind: edgeKind}
	for _, n := range g.Nodes {
		out.Nodes = append(out.Nodes, nodes.GraphNode{Key: n.Key, Kind: n.Kind, Name: n.Name})
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, nodes.GraphEdge{From: e.From, To: e.To, Type: e.Type, Optional: e.Optional, Include: e.Include})
	}
	return out
}
