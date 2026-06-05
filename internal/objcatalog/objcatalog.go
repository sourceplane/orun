package objcatalog

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
)

// CatalogView is one resolved catalog, read back from the object graph. It is
// presentation-neutral — the catalog analogue of objread.ExecutionView.
type CatalogView struct {
	SourceID     string                     // edge: the source it was resolved against (freshness gate)
	HumanKey     string                     // the catalog snapshot's human key
	Components   []CatalogComponentView     // catalog members, sorted by component key
	Graph        map[string]GraphView       // edgeKind → graph slice (dependencies, systems, …)
	Ownership    *OwnershipView             // nil when impact/ is absent (older catalogs)
	Fingerprints map[string]FingerprintView // componentKey → stored input fingerprint; nil when absent
	ObjectID     objectstore.ObjectID       // the catalog Merkle root this view was read from
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
// deep Metadata/Spec blocks are carried verbatim so a richer consumer can reach
// any resolved field; the scalar fields are projected for convenience.
type CatalogComponentView struct {
	ComponentKey string
	Name         string
	Namespace    string
	Repo         string
	Path         string
	Type         string
	Domain       string
	Environments map[string]EnvView
	DependsOn    []string
	Metadata     map[string]any
	Spec         map[string]any
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
	From string
	To   string
	Type string
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
			sawCatalog = true
		case e.Name == dirComponents && e.Kind == objectstore.KindTree:
			comps, cerr := r.readComponents(ctx, e.ID)
			if cerr != nil {
				return CatalogView{}, cerr
			}
			view.Components = comps
		case e.Name == dirGraph && e.Kind == objectstore.KindTree:
			graph, gerr := r.readGraph(ctx, e.ID)
			if gerr != nil {
				return CatalogView{}, gerr
			}
			view.Graph = graph
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
		out = append(out, componentView(m))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ComponentKey < out[j].ComponentKey })
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
			gv.Edges = append(gv.Edges, GraphEdgeView{From: ed.From, To: ed.To, Type: ed.Type})
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

// componentView flattens a manifest into the rendering view, projecting the
// scalar fields out of the verbatim spec while carrying spec/metadata whole.
func componentView(m nodes.ComponentManifest) CatalogComponentView {
	v := CatalogComponentView{
		ComponentKey: m.Identity.ComponentKey,
		Name:         m.Identity.Name,
		Namespace:    m.Identity.Namespace,
		Repo:         m.Identity.Repo,
		Path:         m.Identity.Path,
		Type:         m.Type,
		Metadata:     m.Metadata,
		Spec:         m.Spec,
	}
	if v.Type == "" {
		v.Type = stringField(m.Spec, "type")
	}
	v.Domain = stringField(m.Spec, "domain")
	v.Environments = environmentViews(m.Spec)
	v.DependsOn = dependsOn(m.Spec)
	return v
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
