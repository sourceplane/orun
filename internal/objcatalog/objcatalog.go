package objcatalog

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// Errors are routed on the shared object-store taxonomy.
var (
	ErrNotFound = objectstore.ErrNotFound
	ErrInvalid  = objectstore.ErrInvalid
)

// Canonical catalog ref name (relative to refs/).
const refCatalogCurrent = "catalogs/current"

// Canonical catalog-tree filenames, mirrored from internal/nodes (unexported
// there) the same way internal/objread redeclares the execution-tree names.
const (
	fileCatalog     = "catalog.json"
	dirComponents   = "components"
	dirGraph        = "graph"
	dirImpact       = "impact"
	fileOwnership   = "ownership.json"
	dirFingerprints = "fingerprints"
	fileRelations   = "relations.json"
	dirEntities     = "entities"
)

// CatalogView is one resolved catalog, read back from the object graph. It is
// presentation-neutral — the catalog analogue of objread.ExecutionView.
type CatalogView struct {
	SourceID     string                     // edge: the source it was resolved against (freshness gate)
	HumanKey     string                     // the catalog snapshot's human key
	Components   []CatalogComponentView     // catalog members, sorted by component key
	Entities     []EntityView               // derived non-Component entities (API/Resource/System/Domain/Group/Composition/Environment/Deployment, SC3–SC8), sorted by (kind, key)
	CountsByKind map[string]int             // per-kind entity counts from catalog.json (SC3); nil for older catalogs
	Graph        map[string]GraphView       // edgeKind → graph slice (dependencies, systems, …)
	Relations    []RelationEdgeView         // the single typed relation graph (relations.json, SC2); nil for older catalogs
	Ownership    *OwnershipView             // nil when impact/ is absent (older catalogs)
	Fingerprints map[string]FingerprintView // componentKey → stored input fingerprint; nil when absent
	ObjectID     objectstore.ObjectID       // the catalog Merkle root this view was read from
}

// EntityView is the read view of one derived multi-kind entity blob under
// entities/<Kind>/ (orun-service-catalog/data-model.md §2/§4).
type EntityView struct {
	Kind        string
	EntityKey   string
	Name        string
	Namespace   string
	Repo        string
	MemberCount int      // components referencing this entity (Step 3 membership)
	Members     []string // the referencing component keys, sorted
	Version     string   // Composition semver (Step 4); "" for other kinds
	Lifecycle   string   // Composition lifecycle stage (Step 4)
	Description string   // git-authored one-line summary (catalog-portal CPF)
	// Envelope blocks carried whole so a describe/docs consumer can render the
	// entity's front page (saas-workspace-overview WO3.1a). Empty when the kind
	// declares none; the Repo kind carries displayName/owner/tags/links and a
	// docs.overview doc_ref here.
	DisplayName string           // metadata.displayName
	Owner       string           // ownership.owner
	Tags        []string         // metadata.tags (spec.tags fallback)
	Links       []map[string]any // links[] (title/url/icon)
	Docs        map[string]any   // docs block, incl. overview={path,sha,digest}
	Metadata    map[string]any   // the verbatim metadata block
}

// RelationEdgeView is one forward edge of the catalog-wide relation graph
// (orun-service-catalog/data-model.md §3), read from relations.json.
type RelationEdgeView struct {
	From     string
	FromKind string
	Type     string
	To       string
	ToKind   string
	Optional bool
	Include  string
	Input    bool // build-input edge: the change engine rescopes From when To changes
}

// FingerprintView is the read view of one impact/fingerprints/<name>.json blob:
// a component's stored input fingerprint (data-model.md §2b). The content-aware
// change source recomputes the current Subtree and compares it to this one.
type FingerprintView struct {
	ComponentKey string
	Dir          string
	Subtree      string
	Files        map[string]string
	GlobalDigest string
}

// CatalogComponentView mirrors a ComponentManifest, flattened for rendering. The
// deep Metadata/Spec/Ownership/Lifecycle/Relations blocks are carried verbatim
// so a richer consumer can reach any resolved field; the scalar fields are
// projected for convenience. Ownership/lifecycle are projected out of the entity
// envelope (orun-service-catalog/data-model.md §2) for `catalog describe`.
type CatalogComponentView struct {
	ComponentKey string
	Name         string
	Namespace    string
	Repo         string
	Path         string
	Type         string
	Domain       string
	System       string
	Owner        string
	OwnerSource  string
	Stage        string
	Tier         string
	Description  string   // git-authored one-line summary (catalog-portal CPF)
	Language     string   // implementation language / runtime (catalog-portal CPF)
	Tags         []string // free-form tags from the component source (CPF)
	Environments map[string]EnvView
	DependsOn    []string
	Metadata     map[string]any
	Ownership    map[string]any
	Lifecycle    map[string]any
	Relations    []RelationView
	Integrations map[string]any
	Extensions   map[string]any
	Docs         map[string]any
	Links        []map[string]any
	Spec         map[string]any
}

// RelationView is one typed edge owned by an entity (data-model.md §3).
type RelationView struct {
	Type     string
	To       string
	ToKind   string
	Optional bool
	Include  string
	Input    bool
}

// EnvView is one component-environment binding (spec.environments.<name>).
type EnvView struct {
	Profile string
	Active  bool
}

// GraphView is one edge-kind slice of the catalog graph.
type GraphView struct {
	Nodes []GraphNodeView
	Edges []GraphEdgeView
}

type GraphNodeView struct {
	Key  string
	Kind string
	Name string
}

type GraphEdgeView struct {
	From     string
	To       string
	Type     string
	Optional bool   // dependency-edge optionality, carried from the resolved catalog
	Include  string // change-detection plan-selection mode ("always"; empty = if-selected)
	Input    bool   // build-input edge: the change engine rescopes From when To changes
}

// OwnershipView is the read view of impact/ownership.json (the ownership map the
// change-detection engine consumes). Its on-disk schema is owned by the writer
// (specs/orun-catalog-state CS3, data-model.md §2); this reader decodes it
// tolerantly so a catalog without impact/ yields a nil *OwnershipView.
type OwnershipView struct {
	SchemaVersion       int
	Components          map[string]string // workspace-relative dir → componentKey
	GlobalPaths         []string
	GlobalBlocks        []string
	StructuralFilenames []string
	IgnoreDirs          []string
}

// ownershipRecord is the on-disk shape of impact/ownership.json. Kept local to
// the reader (the writer-side nodes type lands in CS3) so objcatalog stays a
// pure read view.
type ownershipRecord struct {
	Kind                string            `json:"kind"`
	SchemaVersion       int               `json:"schemaVersion"`
	Components          map[string]string `json:"components"`
	GlobalPaths         []string          `json:"globalPaths"`
	GlobalBlocks        []string          `json:"globalBlocks"`
	StructuralFilenames []string          `json:"structuralFilenames"`
	IgnoreDirs          []string          `json:"ignoreDirs"`
}

// fingerprintRecord is the on-disk shape of impact/fingerprints/<name>.json.
type fingerprintRecord struct {
	Kind          string            `json:"kind"`
	SchemaVersion int               `json:"schemaVersion"`
	ComponentKey  string            `json:"componentKey"`
	Dir           string            `json:"dir"`
	Subtree       string            `json:"subtree"`
	Files         map[string]string `json:"files"`
	GlobalDigest  string            `json:"globalDigest"`
}

// Reader reconstructs a CatalogView over one object/ref store pair rooted at the
// object-model root (the directory holding objects/ and refs/).
type Reader struct {
	store objectstore.ObjectStore
	refs  refstore.RefStore
}

// New constructs a Reader. The root is implied by the stores; objcatalog needs
// no working-tree access, so (unlike objread) it carries no root path.
func New(store objectstore.ObjectStore, refs refstore.RefStore) *Reader {
	return &Reader{store: store, refs: refs}
}

// Load reads one catalog into a CatalogView. ref may be a catalog ref name
// (default "catalogs/current") or a bare catalog Merkle id. A missing impact/
// subtree is not an error — Ownership is left nil.
func (r *Reader) Load(ctx context.Context, ref string) (CatalogView, error) {
	root, err := r.resolve(ctx, ref)
	if err != nil {
		return CatalogView{}, err
	}

	entries, err := r.store.GetTree(ctx, root)
	if err != nil {
		return CatalogView{}, err
	}

	view := CatalogView{ObjectID: root, Graph: map[string]GraphView{}}
	var sawCatalog bool
	for _, e := range entries {
		switch {
		case e.Name == fileCatalog && e.Kind == objectstore.KindBlob:
			snap, derr := decodeBlob[nodes.CatalogSnapshot](ctx, r.store, e.ID)
			if derr != nil {
				return CatalogView{}, derr
			}
			view.SourceID = snap.SourceID
			view.HumanKey = snap.HumanKey
			view.CountsByKind = snap.CountsByKind
			sawCatalog = true
		case e.Name == dirComponents && e.Kind == objectstore.KindTree:
			comps, cerr := r.readComponents(ctx, e.ID)
			if cerr != nil {
				return CatalogView{}, cerr
			}
			view.Components = comps
		case e.Name == dirEntities && e.Kind == objectstore.KindTree:
			ents, eerr := r.readEntities(ctx, e.ID)
			if eerr != nil {
				return CatalogView{}, eerr
			}
			view.Entities = ents
		case e.Name == dirGraph && e.Kind == objectstore.KindTree:
			graph, gerr := r.readGraph(ctx, e.ID)
			if gerr != nil {
				return CatalogView{}, gerr
			}
			view.Graph = graph
		case e.Name == fileRelations && e.Kind == objectstore.KindBlob:
			rg, rerr := decodeBlob[nodes.RelationGraph](ctx, r.store, e.ID)
			if rerr != nil {
				return CatalogView{}, rerr
			}
			view.Relations = make([]RelationEdgeView, 0, len(rg.Edges))
			for _, ed := range rg.Edges {
				view.Relations = append(view.Relations, RelationEdgeView{
					From: ed.From, FromKind: ed.FromKind, Type: ed.Type,
					To: ed.To, ToKind: ed.ToKind, Optional: ed.Optional, Include: ed.Include,
					Input: ed.Input,
				})
			}
		case e.Name == dirImpact && e.Kind == objectstore.KindTree:
			own, fps, ierr := r.readImpact(ctx, e.ID)
			if ierr != nil {
				return CatalogView{}, ierr
			}
			view.Ownership = own
			view.Fingerprints = fps
		}
	}
	if !sawCatalog {
		return CatalogView{}, fmt.Errorf("objcatalog: %w: no %s in %s", ErrInvalid, fileCatalog, root)
	}
	return view, nil
}

// resolve turns a ref name or bare catalog id into a catalog tree root id. A
// value shaped like an object id ("<algo>:<hex>") is taken verbatim; anything
// else (incl. "") is read from the ref store, defaulting to catalogs/current.
func (r *Reader) resolve(ctx context.Context, ref string) (objectstore.ObjectID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = refCatalogCurrent
	}
	if isObjectID(ref) {
		return objectstore.ObjectID(ref), nil
	}
	got, err := r.refs.Read(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("objcatalog: catalog %q: %w", ref, ErrNotFound)
	}
	return objectstore.ObjectID(got.Target), nil
}

// readComponents decodes every components/<name>.json blob into a sorted slice
// of component views.
func (r *Reader) readComponents(ctx context.Context, treeID objectstore.ObjectID) ([]CatalogComponentView, error) {
	entries, err := r.store.GetTree(ctx, treeID)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogComponentView, 0, len(entries))
	for _, e := range entries {
		if e.Kind != objectstore.KindBlob {
			continue
		}
		m, derr := decodeBlob[nodes.ComponentManifest](ctx, r.store, e.ID)
		if derr != nil {
			return nil, derr
		}
		upConvertManifest(&m)
		out = append(out, componentView(m))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ComponentKey < out[j].ComponentKey })
	return out, nil
}

// readEntities walks the entities/<Kind>/ subtree and decodes every derived
// entity blob into a flat, sorted view (sorted by kind then entityKey). The
// kind label comes from the decoded blob, independent of the directory name.
func (r *Reader) readEntities(ctx context.Context, treeID objectstore.ObjectID) ([]EntityView, error) {
	kindTrees, err := r.store.GetTree(ctx, treeID)
	if err != nil {
		return nil, err
	}
	var out []EntityView
	for _, kt := range kindTrees {
		if kt.Kind != objectstore.KindTree {
			continue
		}
		blobs, berr := r.store.GetTree(ctx, kt.ID)
		if berr != nil {
			return nil, berr
		}
		for _, b := range blobs {
			if b.Kind != objectstore.KindBlob {
				continue
			}
			e, derr := decodeBlob[nodes.Entity](ctx, r.store, b.ID)
			if derr != nil {
				return nil, derr
			}
			ev := EntityView{
				Kind:      e.Kind,
				EntityKey: e.Identity.EntityKey,
				Name:      e.Identity.Name,
				Namespace: e.Identity.Namespace,
				Repo:      e.Identity.Repo,
			}
			ev.MemberCount = intField(e.Spec, "memberCount")
			ev.Members = stringSliceField(e.Spec, "members")
			ev.Version = stringField(e.Spec, "version")
			ev.Lifecycle = stringField(e.Spec, "lifecycle")
			ev.Description = stringField(e.Spec, "description")
			if ev.Description == "" {
				ev.Description = stringField(e.Metadata, "description")
			}
			// Envelope blocks for the entity front page (WO3.1a).
			ev.DisplayName = stringField(e.Metadata, "displayName")
			ev.Owner = stringField(e.Ownership, "owner")
			ev.Tags = stringSliceField(e.Metadata, "tags")
			if len(ev.Tags) == 0 {
				ev.Tags = stringSliceField(e.Spec, "tags")
			}
			ev.Links = e.Links
			ev.Docs = e.Docs
			ev.Metadata = e.Metadata
			out = append(out, ev)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].EntityKey < out[j].EntityKey
	})
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// readGraph decodes every graph/<edgeKind>.json blob, keyed by the slice's own
// EdgeKind (the authoritative label, independent of the filename).
func (r *Reader) readGraph(ctx context.Context, treeID objectstore.ObjectID) (map[string]GraphView, error) {
	entries, err := r.store.GetTree(ctx, treeID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]GraphView, len(entries))
	for _, e := range entries {
		if e.Kind != objectstore.KindBlob {
			continue
		}
		g, derr := decodeBlob[nodes.CatalogGraph](ctx, r.store, e.ID)
		if derr != nil {
			return nil, derr
		}
		gv := GraphView{}
		for _, n := range g.Nodes {
			gv.Nodes = append(gv.Nodes, GraphNodeView{Key: n.Key, Kind: n.Kind, Name: n.Name})
		}
		for _, ed := range g.Edges {
			gv.Edges = append(gv.Edges, GraphEdgeView{From: ed.From, To: ed.To, Type: ed.Type, Optional: ed.Optional, Include: ed.Include, Input: ed.Input})
		}
		out[g.EdgeKind] = gv
	}
	return out, nil
}

// readImpact decodes the impact/ subtree: ownership.json (→ *OwnershipView, nil
// when absent) and the fingerprints/ subtree (→ map keyed by componentKey, nil
// when absent). Both are tolerant — a subtree missing either yields nil for that
// part, never an error.
func (r *Reader) readImpact(ctx context.Context, treeID objectstore.ObjectID) (*OwnershipView, map[string]FingerprintView, error) {
	entries, err := r.store.GetTree(ctx, treeID)
	if err != nil {
		return nil, nil, err
	}
	var own *OwnershipView
	var fps map[string]FingerprintView
	for _, e := range entries {
		switch {
		case e.Name == fileOwnership && e.Kind == objectstore.KindBlob:
			rec, derr := decodeBlob[ownershipRecord](ctx, r.store, e.ID)
			if derr != nil {
				return nil, nil, derr
			}
			own = &OwnershipView{
				SchemaVersion:       rec.SchemaVersion,
				Components:          rec.Components,
				GlobalPaths:         rec.GlobalPaths,
				GlobalBlocks:        rec.GlobalBlocks,
				StructuralFilenames: rec.StructuralFilenames,
				IgnoreDirs:          rec.IgnoreDirs,
			}
		case e.Name == dirFingerprints && e.Kind == objectstore.KindTree:
			f, ferr := r.readFingerprints(ctx, e.ID)
			if ferr != nil {
				return nil, nil, ferr
			}
			fps = f
		}
	}
	return own, fps, nil
}

// readFingerprints decodes every fingerprints/<name>.json blob, keyed by the
// stored componentKey. An empty subtree yields nil.
func (r *Reader) readFingerprints(ctx context.Context, treeID objectstore.ObjectID) (map[string]FingerprintView, error) {
	entries, err := r.store.GetTree(ctx, treeID)
	if err != nil {
		return nil, err
	}
	out := map[string]FingerprintView{}
	for _, e := range entries {
		if e.Kind != objectstore.KindBlob {
			continue
		}
		rec, derr := decodeBlob[fingerprintRecord](ctx, r.store, e.ID)
		if derr != nil {
			return nil, derr
		}
		out[rec.ComponentKey] = FingerprintView{
			ComponentKey: rec.ComponentKey,
			Dir:          rec.Dir,
			Subtree:      rec.Subtree,
			Files:        rec.Files,
			GlobalDigest: rec.GlobalDigest,
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// componentView flattens an entity-envelope manifest into the rendering view,
// projecting the scalar fields out of the verbatim envelope while carrying the
// deep blocks whole. Dependencies and system/domain membership are read from the
// typed relations (they no longer live inline in spec); owner/lifecycle are read
// from the ownership/lifecycle blocks.
// upConvertManifest lazily up-converts a pre-v1 (flat, pre-SC1) manifest blob to
// the v1 envelope shape ON READ, never mutating the stored blob (invariant #7,
// migration.md §2). Old immutable snapshots keep their bytes; a read of one just
// sees the v1 shape. Within-v1 evolution is additive (missing blocks read as
// absent), so only the one flat→envelope reshape needs handling here:
//
//   - the flat manifest carried `owner` inside metadata and had no ownership
//     block → synthesize ownership{owner, source:authored};
//   - lifecycle lived in spec.lifecycle with no lifecycle block → synthesize it;
//   - stamp apiVersion to v1 (catalogmodel up-convert seam).
//
// A blob already in the v1 envelope shape (ownership present) is left untouched.
func upConvertManifest(m *nodes.ComponentManifest) {
	if v, ok := catalogmodel.UpConvertAPIVersion(m.APIVersion); ok {
		m.APIVersion = v
	} else if m.APIVersion == "" {
		m.APIVersion = catalogmodel.APIVersionV1
	}
	// Already v1-shaped (the resolver always emits an ownership block).
	if m.Ownership != nil {
		return
	}
	// Pre-SC1 flat shape: owner lived in metadata.
	if owner := stringField(m.Metadata, "owner"); owner != "" {
		key, _ := catalogmodel.NormalizeOwnerRef(owner)
		m.Ownership = map[string]any{"owner": key, "source": catalogmodel.OwnershipSourceAuthored}
		delete(m.Metadata, "owner")
	}
	if m.Lifecycle == nil {
		if stage := stringField(m.Spec, "lifecycle"); stage != "" {
			m.Lifecycle = map[string]any{"stage": stage, "maturity": nil}
		}
	}
}

func componentView(m nodes.ComponentManifest) CatalogComponentView {
	v := CatalogComponentView{
		ComponentKey: m.Identity.ComponentKey,
		Name:         m.Identity.Name,
		Namespace:    m.Identity.Namespace,
		Repo:         m.Identity.Repo,
		Path:         m.Identity.Path,
		Type:         m.Type,
		Metadata:     m.Metadata,
		Ownership:    m.Ownership,
		Lifecycle:    m.Lifecycle,
		Integrations: m.Integrations,
		Extensions:   m.Extensions,
		Docs:         m.Docs,
		Links:        m.Links,
		Spec:         m.Spec,
	}
	if v.Type == "" {
		v.Type = stringField(m.Spec, "type")
	}
	// Ownership/lifecycle are projected from the envelope blocks (SC1 reshape).
	v.Owner = stringField(m.Ownership, "owner")
	v.OwnerSource = stringField(m.Ownership, "source")
	v.Stage = stringField(m.Lifecycle, "stage")
	v.Tier = stringField(m.Lifecycle, "tier")
	// System/Domain/Environments/DependsOn still read from the (lossless) spec
	// block; SC2 promotes the dependency edges fully to relations.json.
	v.System = stringField(m.Spec, "system")
	v.Domain = stringField(m.Spec, "domain")
	v.Environments = environmentViews(m.Spec)
	v.DependsOn = dependsOn(m.Spec)
	v.Relations = relationViews(m.Relations)
	// Git-authored portal fields (CPF): read from the lossless spec/metadata/docs
	// blocks with a tolerant fallback chain; empty when the source declares none.
	v.Description = firstString(m.Spec, m.Metadata, m.Docs, []string{"description", "description", "summary"})
	v.Language = stringField(m.Spec, "language")
	if v.Language == "" {
		v.Language = stringField(m.Metadata, "language")
	}
	if v.Language == "" {
		if langs := stringSliceField(m.Spec, "languages"); len(langs) > 0 {
			v.Language = langs[0]
		}
	}
	v.Tags = stringSliceField(m.Spec, "tags")
	if len(v.Tags) == 0 {
		v.Tags = stringSliceField(m.Metadata, "tags")
	}
	return v
}

// firstString returns the first non-empty value of keys[i] read from maps[i],
// in order — the tolerant precedence chain for git-authored portal fields.
func firstString(m1, m2, m3 map[string]any, keys []string) string {
	maps := []map[string]any{m1, m2, m3}
	for i, m := range maps {
		if i >= len(keys) {
			break
		}
		if s := stringField(m, keys[i]); s != "" {
			return s
		}
	}
	return ""
}

// relationViews carries the typed envelope relations into the read view for
// richer consumers (the portal/graph). The CLI/TUI convenience scalars
// (DependsOn/System/Domain) still derive from spec in SC1.
func relationViews(rels []nodes.EntityRelation) []RelationView {
	if len(rels) == 0 {
		return nil
	}
	out := make([]RelationView, 0, len(rels))
	for _, r := range rels {
		out = append(out, RelationView{Type: r.Type, To: r.To, ToKind: r.ToKind, Optional: r.Optional, Include: r.Include, Input: r.Input})
	}
	return out
}

// dependsOn reads the component keys under spec.dependencies.components[].key,
// sorted for determinism.
func dependsOn(spec map[string]any) []string {
	deps, ok := spec["dependencies"].(map[string]any)
	if !ok {
		return nil
	}
	comps, ok := deps["components"].([]any)
	if !ok || len(comps) == 0 {
		return nil
	}
	out := make([]string, 0, len(comps))
	for _, raw := range comps {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if key := stringField(c, "key"); key != "" {
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// environmentViews reads spec.environments.<name> = {profile, active}.
func environmentViews(spec map[string]any) map[string]EnvView {
	envs, ok := spec["environments"].(map[string]any)
	if !ok || len(envs) == 0 {
		return nil
	}
	out := make(map[string]EnvView, len(envs))
	for name, raw := range envs {
		body, _ := raw.(map[string]any)
		out[name] = EnvView{
			Profile: stringField(body, "profile"),
			Active:  boolField(body, "active"),
		}
	}
	return out
}

func decodeBlob[T any](ctx context.Context, s objectstore.ObjectStore, id objectstore.ObjectID) (T, error) {
	var zero T
	_, body, err := s.Get(ctx, id)
	if err != nil {
		return zero, err
	}
	return nodes.Decode[T](body)
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// intField reads a numeric field (JSON numbers decode as float64).
func intField(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// stringSliceField reads a []string from a generic []any field.
func stringSliceField(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

// isObjectID reports whether s looks like an "<algo>:<hex>" content id rather
// than a ref name. Catalog ref names ("catalogs/current") contain a slash and no
// such prefix, so the check is unambiguous for the inputs Load accepts.
func isObjectID(s string) bool {
	i := strings.IndexByte(s, ':')
	if i <= 0 || strings.Contains(s, "/") {
		return false
	}
	hex := s[i+1:]
	if hex == "" {
		return false
	}
	for _, c := range hex {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
