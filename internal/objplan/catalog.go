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
func BuildCatalogNodes(view *catalogresolve.CatalogView, resolverVersion int, ownerResolver OwnerResolver, compositionResolver CompositionResolver) (nodes.CatalogSnapshot, []nodes.ComponentManifest, []nodes.CatalogGraph, nodes.ImpactOwnership, []nodes.ComponentFingerprint) {
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
			manifests = append(manifests, mapEntity(cm, resolverVersion, ownerResolver, compositionResolver))
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

	// Emit the declared Repo entity (WO3): a repo self-describing, from the
	// top-level `repo:` block. Absent when no block is declared, so existing
	// catalogs are unchanged.
	if view != nil && view.ResolvedCatalog != nil && view.RepoDecl != nil {
		cat.DeclaredEntities = []nodes.Entity{repoEntity(view.RepoDecl)}
	}

	return cat, manifests, graphs, buildOwnership(view), buildFingerprints(view)
}

// repoEntity maps a resolved RepoDeclaration into the Repo node entity. Docs,
// links, and owner land in the entity's dedicated blocks so the platform's
// projector reads them the same way it reads a component's.
func repoEntity(d *catalogresolve.RepoDeclaration) nodes.Entity {
	e := nodes.Entity{
		APIVersion: "orun.io/v1",
		Kind:       nodes.EntityKindRepo,
		Identity: nodes.EntityIdentity{
			EntityKey: d.EntityKey,
			Kind:      nodes.EntityKindRepo,
			Name:      d.Name,
			Namespace: d.Namespace,
			Repo:      d.Repo,
		},
	}
	meta := map[string]any{}
	putNonEmpty(meta, "displayName", d.DisplayName)
	putNonEmpty(meta, "description", d.Description)
	if len(d.Tags) > 0 {
		meta["tags"] = strSliceToAny(d.Tags)
	}
	if len(meta) > 0 {
		e.Metadata = meta
	}
	if d.Owner != "" {
		e.Ownership = map[string]any{"owner": d.Owner}
	}
	if d.Overview != "" {
		e.Docs = map[string]any{"overview": d.Overview}
	}
	if len(d.Links) > 0 {
		links := make([]map[string]any, 0, len(d.Links))
		for _, l := range d.Links {
			lm := map[string]any{}
			putNonEmpty(lm, "title", l.Title)
			putNonEmpty(lm, "url", l.URL)
			putNonEmpty(lm, "icon", l.Icon)
			links = append(links, lm)
		}
		e.Links = links
	}
	return e
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
func mapEntity(cm *catalogmodel.ComponentManifest, resolverVersion int, ownerResolver OwnerResolver, compositionResolver CompositionResolver) nodes.ComponentManifest {
	own := resolveOwner(cm, ownerResolver)
	var comp *CompositionMeta
	if compositionResolver != nil && cm.Spec.Type != "" {
		comp = compositionResolver(cm.Spec.Type)
	}
	spec := toMap(cm.Spec)
	// The legacy zero CompositionRef serializes as {"source":""} — drop it so an
	// unbound component carries no composition block at all (a real binding always
	// sets a non-empty source).
	if c, ok := spec["composition"].(map[string]any); ok {
		if s, _ := c["source"].(string); s == "" {
			delete(spec, "composition")
		}
	}
	if comp != nil {
		if spec == nil {
			spec = map[string]any{}
		}
		spec["composition"] = compositionSpecMap(comp)
	}
	// Surface the inferred runtime profile (languages/frameworks/infra/package
	// managers) on the envelope spec (data-model.md §4) — a queryable portal
	// facet the resolver computes but SC1 dropped. Omitted when nothing inferred.
	if rt := runtimeBlock(cm.Runtime.Inferred); rt != nil {
		if spec == nil {
			spec = map[string]any{}
		}
		spec["runtime"] = rt
	}
	// SC8: a backing composition's effects.integrations declaration populates the
	// component's integrations (the golden path registers the service); authored
	// integrations (SC6) still win on a key conflict.
	integrations := cm.Integrations
	if comp != nil && comp.Effects != nil {
		integrations = mergeIntegrations(cm.Integrations, comp.Effects.Integrations)
	}
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
		Spec:         spec,
		Relations:    entityRelations(cm, own, comp),
		Contracts:    entityContracts(cm.Spec.Dependencies.APIs, exposedAPIs(comp), cm.Identity.Namespace, cm.Identity.Repo),
		Integrations: integrations,
		Docs:         docsBlock(cm.Docs),
		Links:        linksBlock(cm.Links),
		Extensions:   cm.Extensions,
		Provenance:   entityProvenance(cm, resolverVersion),
	}
	return m
}

// compositionSpecMap projects a composition binding onto the spec.composition
// block (data-model.md §5): the source name + content digest + source pointer,
// plus the SC8 effects *declaration* (what the golden path produces).
func compositionSpecMap(c *CompositionMeta) map[string]any {
	out := map[string]any{"source": c.Name}
	putNonEmpty(out, "digest", c.Digest)
	putNonEmpty(out, "version", c.Version)
	putNonEmpty(out, "lifecycle", c.Lifecycle)
	putNonEmpty(out, "sourceKind", c.SourceKind)
	putNonEmpty(out, "sourceRef", c.SourceRef)
	putNonEmpty(out, "sourcePath", c.SourcePath)
	if c.Effects != nil {
		eff := map[string]any{}
		if len(c.Effects.Integrations) > 0 {
			eff["integrations"] = c.Effects.Integrations
		}
		if len(c.Effects.Scorecards) > 0 {
			eff["scorecards"] = c.Effects.Scorecards
		}
		if len(c.Effects.Provides) > 0 {
			eff["provides"] = strSliceToAny(c.Effects.Provides)
		}
		if len(c.Effects.Exposes) > 0 {
			eff["exposes"] = strSliceToAny(c.Effects.Exposes)
		}
		if len(eff) > 0 {
			out["effects"] = eff
		}
	}
	return out
}

// runtimeBlock projects the inferred runtime traits onto the spec.runtime shape
// (data-model.md §4). Returns nil when nothing was inferred so the field is
// omitted rather than emitting empty arrays.
func runtimeBlock(inf catalogmodel.ComponentInferred) map[string]any {
	out := map[string]any{}
	if len(inf.Languages) > 0 {
		out["languages"] = strSliceToAny(inf.Languages)
	}
	if len(inf.Frameworks) > 0 {
		out["frameworks"] = strSliceToAny(inf.Frameworks)
	}
	if len(inf.Infra) > 0 {
		out["infra"] = strSliceToAny(inf.Infra)
	}
	if len(inf.PackageManagers) > 0 {
		out["packageManagers"] = strSliceToAny(inf.PackageManagers)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeIntegrations overlays the golden-path-declared integrations under any
// authored ones (authored wins). Returns nil when both are empty.
func mergeIntegrations(authored, declared map[string]any) map[string]any {
	if len(authored) == 0 && len(declared) == 0 {
		return nil
	}
	out := make(map[string]any, len(authored)+len(declared))
	for k, v := range declared {
		out[k] = v
	}
	for k, v := range authored {
		out[k] = v
	}
	return out
}

// docsBlock projects the docs pointers onto the envelope's generic docs map.
func docsBlock(d *catalogmodel.ComponentDocs) map[string]any {
	if d == nil {
		return nil
	}
	out := map[string]any{}
	putNonEmpty(out, "overview", d.Overview)
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
	owner      string   // normalized typed ref (group:/user:) or "unknown"
	ownerKind  string   // Group | User ("" when unknown)
	additional []string // normalized typed refs
	source     string   // authored | CODEOWNERS | unknown
}

// resolveOwner applies the ownership precedence (design.md §4.3, S-2): an
// authored metadata.owner wins (source=authored); otherwise CODEOWNERS over the
// component's source path supplies it (source=CODEOWNERS); otherwise the entity
// is unowned (source=unknown) — flagged, never silently false-owned. The owner
// and additionalOwners are normalized to typed refs (group:/user:, §3).
func resolveOwner(cm *catalogmodel.ComponentManifest, ownerResolver OwnerResolver) resolvedOwner {
	mk := func(primary string, rest []string, source string) resolvedOwner {
		key, kind := catalogmodel.NormalizeOwnerRef(primary)
		add := make([]string, 0, len(rest))
		for _, r := range rest {
			if k, _ := catalogmodel.NormalizeOwnerRef(r); k != "" {
				add = append(add, k)
			}
		}
		return resolvedOwner{owner: key, ownerKind: kind, additional: add, source: source}
	}
	if cm.Metadata.Owner != "" {
		return mk(cm.Metadata.Owner, cm.Metadata.Maintainers, catalogmodel.OwnershipSourceAuthored)
	}
	if ownerResolver != nil {
		if owners := ownerResolver(cm.Identity.SourceFile); len(owners) > 0 {
			return mk(owners[0], append(append([]string(nil), owners[1:]...), cm.Metadata.Maintainers...), catalogmodel.OwnershipSourceCODEOWNERS)
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
func entityRelations(cm *catalogmodel.ComponentManifest, own resolvedOwner, comp *CompositionMeta) []nodes.EntityRelation {
	ns, repo := cm.Identity.Namespace, cm.Identity.Repo
	qualify := func(v string) string { return catalogmodel.QualifyEntityKey(ns, repo, v) }

	var rels []nodes.EntityRelation
	if own.source != catalogmodel.OwnershipSourceUnknown && own.owner != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeOwnedBy, To: own.owner, ToKind: own.ownerKind,
		})
	}
	if comp != nil && comp.Name != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypeComposedBy, To: qualify(comp.Name), ToKind: catalogmodel.EntityKindComposition,
		})
	}
	if cm.Spec.System != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypePartOf, To: qualify(cm.Spec.System), ToKind: catalogmodel.EntityKindSystem,
		})
	}
	if cm.Spec.Domain != "" {
		rels = append(rels, nodes.EntityRelation{
			Type: catalogmodel.RelTypePartOf, To: qualify(cm.Spec.Domain), ToKind: catalogmodel.EntityKindDomain,
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
			Type: catalogmodel.RelTypeDependsOn, To: qualify(r), ToKind: catalogmodel.EntityKindResource,
		})
	}
	// SC8 effects.graph: a backing composition that provisions Resources makes
	// each backed component dependOn them (the golden path *produces* the
	// resource the component runs against).
	if comp != nil && comp.Effects != nil {
		for _, r := range comp.Effects.Provides {
			rels = append(rels, nodes.EntityRelation{
				Type: catalogmodel.RelTypeDependsOn, To: qualify(r), ToKind: catalogmodel.EntityKindResource,
			})
		}
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
			Type: catalogmodel.RelTypeDeployedTo, To: qualify(env), ToKind: catalogmodel.EntityKindEnvironment,
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

// exposedAPIs returns the API keys a backing composition exposes (SC8
// effects.exposes), or nil. Each becomes a provided API on the backed component.
func exposedAPIs(comp *CompositionMeta) []string {
	if comp == nil || comp.Effects == nil {
		return nil
	}
	return comp.Effects.Exposes
}

// entityContracts builds the contracts block from the resolved provided/consumed
// APIs (data-model.md §2) plus any composition-exposed APIs (SC8
// effects.exposes), qualifying bare api refs to namespaced keys. The provided
// set is deduped + sorted. Returns nil when no APIs are declared.
func entityContracts(apis catalogmodel.APIDependencies, exposed []string, namespace, repo string) map[string]any {
	qual := func(a string) string { return catalogmodel.QualifyEntityKey(namespace, repo, a) }
	side := func(refs []string) []any {
		out := make([]any, 0, len(refs))
		for _, a := range refs {
			out = append(out, map[string]any{"api": qual(a)})
		}
		return out
	}
	// Merge authored provides + composition-exposed APIs (deduped, sorted).
	provSet := map[string]bool{}
	for _, a := range apis.Provides {
		provSet[qual(a)] = true
	}
	for _, a := range exposed {
		provSet[qual(a)] = true
	}
	provides := make([]string, 0, len(provSet))
	for k := range provSet {
		provides = append(provides, k)
	}
	sort.Strings(provides)

	out := map[string]any{}
	if len(provides) > 0 {
		pv := make([]any, 0, len(provides))
		for _, a := range provides {
			pv = append(pv, map[string]any{"api": a})
		}
		out["provides"] = pv
	}
	if len(apis.Consumes) > 0 {
		out["consumes"] = side(apis.Consumes)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// entityProvenance carries the inheritance/inference trail plus the resolver
// stamp. Deterministic from inputs, so it folds stably into the catalog id.
func entityProvenance(cm *catalogmodel.ComponentManifest, resolverVersion int) map[string]any {
	res := cm.Resolution
	out := map[string]any{
		"resolver": map[string]any{
			"resolverVersion": resolverVersion,
			"schemaVersion":   catalogmodel.APIVersionV1,
		},
	}
	// The resolver's canonical manifestHash (computed over the resolved manifest
	// minus provenance/source, identity-and-keys.md §10) — the spec's
	// provenance.manifestHash field (data-model.md §2).
	putNonEmpty(out, "manifestHash", cm.Source.ManifestHash)
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
