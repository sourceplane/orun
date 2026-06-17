package objmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objread"
)

// Errors are routed on the shared object-store taxonomy so callers handle a
// model-read miss the same way they handle an object or ref miss.
var (
	ErrNotFound = objectstore.ErrNotFound
	ErrInvalid  = objectstore.ErrInvalid
)

// Canonical default ref names, relative to refs/. Selecting a source/head is
// resolving one of these (or a scope-specific sibling such as
// "catalogs/branches/<b>" or "executions/by-id/<id>").
const (
	refCatalogCurrent   = "catalogs/current"
	refRevisionsLatest  = "revisions/latest"
	refExecutionsLatest = "executions/latest"
)

// Catalog/revision tree filenames, mirrored from internal/nodes (unexported
// there) the same way objread/objcatalog redeclare the tree filenames they read.
const (
	fileRevision = "revision.json"
	filePlan     = "plan.json"
)

// Re-exported view types so a ModelReader consumer depends only on this package.
// Catalog and execution views are the presentation-neutral shapes the per-layer
// readers already produce; rendering layers project them into their own
// view-models.
type (
	// ObjectID is the content address of an object in the graph.
	ObjectID = objectstore.ObjectID
	// CatalogView is one resolved catalog (components + entities + graph).
	CatalogView = objcatalog.CatalogView
	// ExecutionView is one execution, sealed or live, with full job detail.
	ExecutionView = objread.ExecutionView
	// Deployment is one (component, environment) live-plane entry.
	Deployment = objread.Deployment
)

// RevisionView is a presentation-neutral compiled revision: the plan that a
// trigger produced, plus the component set it touches. It is the revision
// analogue of objcatalog.CatalogView / objread.ExecutionView.
type RevisionView struct {
	ObjectID   ObjectID // the revision tree Merkle root this view was read from
	HumanKey   string
	CatalogID  string
	SourceID   string
	PlanHash   string
	ScopeMode  string   // RevisionScope.Mode (full|changed|…), "" for older revisions
	JobCount   int      // jobs in the compiled plan
	PlanName   string   // the plan's display name (plan.metadata.name)
	Components []string // distinct component names in plan order
}

// HistoryView is one component's change/run history across the whole graph: the
// executions that ran it (newest-first) and the latest execution per
// environment (the live plane). It is the projection a component page renders.
type HistoryView struct {
	ComponentKey string
	Executions   []ExecutionView
	Deployments  []Deployment
}

// ExecSummary is a lean execution header for listings — no per-job detail.
// ListExecutions returns these; resolve one to a full ExecutionView with
// Execution when a row is opened.
type ExecSummary struct {
	ExecutionID  string
	ExecutionKey string
	RevisionID   string
	TriggerID    string
	Status       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	Live         bool
	Summary      nodes.ExecSummary
	ObjectID     ObjectID // sealed root; empty when Live
}

// Filter narrows a ListExecutions query. The zero value lists every execution
// newest-first (live runs ahead of sealed ones).
type Filter struct {
	// Status, when set, keeps only executions with this exact status.
	Status string
	// Component, when set, keeps only executions whose compiled plan included
	// this component key/name (the scan+filter join objread already serves).
	Component string
	// Limit caps the result count (0 = no cap). Applied after filtering.
	Limit int
}

// ModelReader is the single read seam over the object graph. Every consumer —
// the TUI cockpit, the CLI porcelain, the hosted console — depends on this and
// nothing lower. ResolveRef turns a head name into a content id; the Catalog /
// Revision / Execution accessors take either a ref name (resolved server- or
// store-side) or a bare object id, so source/head selection is "pass the ref".
type ModelReader interface {
	// ResolveRef returns the object id a head ref points at (e.g.
	// "catalogs/main", "sources/prs/139", "executions/latest").
	ResolveRef(ctx context.Context, name string) (ObjectID, error)
	// Catalog reads the catalog at ref (default "catalogs/current").
	Catalog(ctx context.Context, ref string) (CatalogView, error)
	// Revision reads the compiled revision at ref (default "revisions/latest").
	Revision(ctx context.Context, ref string) (RevisionView, error)
	// Execution reads one execution by ref/id (default "executions/latest").
	Execution(ctx context.Context, ref string) (ExecutionView, error)
	// ComponentHistory returns one component's executions + live deployments.
	ComponentHistory(ctx context.Context, componentKey string) (HistoryView, error)
	// ListExecutions returns execution headers newest-first, filtered.
	ListExecutions(ctx context.Context, filter Filter) ([]ExecSummary, error)
}

// Reader implements ModelReader by composing the shipped per-layer readers over
// one object/ref store pair. root is the object-model root (the directory
// holding objects/, refs/, run/); for a remote-backed Reader with no local
// working tree it may be "" — live-run lookups then simply find nothing.
type Reader struct {
	store objectstore.ObjectStore
	refs  refstore.RefStore
	exec  *objread.Reader
	cat   *objcatalog.Reader
}

// NewReader constructs a Reader. Pass a local object/ref store for the on-disk
// model, or a remote-backed pair for the hosted model — the type is identical.
func NewReader(store objectstore.ObjectStore, refs refstore.RefStore, root string) *Reader {
	return &Reader{
		store: store,
		refs:  refs,
		exec:  objread.New(store, refs, root),
		cat:   objcatalog.New(store, refs),
	}
}

var _ ModelReader = (*Reader)(nil)

// ResolveRef returns the target of a head ref.
func (r *Reader) ResolveRef(ctx context.Context, name string) (ObjectID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("objmodel: %w: empty ref name", ErrInvalid)
	}
	ref, err := r.refs.Read(ctx, name)
	if err != nil {
		return "", fmt.Errorf("objmodel: resolve ref %q: %w", name, err)
	}
	return objectstore.ObjectID(ref.Target), nil
}

// Catalog reads the catalog at a ref name or bare catalog id.
func (r *Reader) Catalog(ctx context.Context, ref string) (CatalogView, error) {
	if strings.TrimSpace(ref) == "" {
		ref = refCatalogCurrent
	}
	return r.cat.Load(ctx, ref)
}

// Execution reads one execution by ref or id (sealed or live).
func (r *Reader) Execution(ctx context.Context, ref string) (ExecutionView, error) {
	return r.exec.Get(ctx, ref)
}

// ComponentHistory joins a component's executions and its live-plane
// deployments into one view. Both halves are bounded by the execution count.
func (r *Reader) ComponentHistory(ctx context.Context, componentKey string) (HistoryView, error) {
	key := strings.TrimSpace(componentKey)
	if key == "" {
		return HistoryView{}, fmt.Errorf("objmodel: %w: empty component key", ErrInvalid)
	}
	execs, err := r.exec.ComponentExecutions(ctx, key)
	if err != nil {
		return HistoryView{}, err
	}
	deps, err := r.exec.ComponentDeployments(ctx, key)
	if err != nil {
		return HistoryView{}, err
	}
	return HistoryView{ComponentKey: key, Executions: execs, Deployments: deps}, nil
}

// ListExecutions returns execution headers newest-first. A component filter
// uses objread's scan+filter join; status and limit are applied on top.
func (r *Reader) ListExecutions(ctx context.Context, filter Filter) ([]ExecSummary, error) {
	var views []ExecutionView
	var err error
	if c := strings.TrimSpace(filter.Component); c != "" {
		views, err = r.exec.ComponentExecutions(ctx, c)
	} else {
		views, err = r.exec.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]ExecSummary, 0, len(views))
	for i := range views {
		if filter.Status != "" && views[i].Status != filter.Status {
			continue
		}
		out = append(out, summaryOf(views[i]))
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

// Revision reads one compiled revision by ref or id (default revisions/latest).
func (r *Reader) Revision(ctx context.Context, ref string) (RevisionView, error) {
	root, err := r.resolveRevision(ctx, ref)
	if err != nil {
		return RevisionView{}, err
	}
	entries, err := r.store.GetTree(ctx, root)
	if err != nil {
		return RevisionView{}, err
	}
	view := RevisionView{ObjectID: root}
	var sawRevision bool
	for _, e := range entries {
		switch e.Name {
		case fileRevision:
			rec, derr := decodeBlob[nodes.PlanRevision](ctx, r.store, e.ID)
			if derr != nil {
				return RevisionView{}, derr
			}
			view.HumanKey = rec.HumanKey
			view.CatalogID = rec.CatalogID
			view.SourceID = rec.SourceID
			view.PlanHash = rec.PlanHash
			view.ScopeMode = rec.Scope.Mode
			view.JobCount = rec.JobCount
			sawRevision = true
		case filePlan:
			_, body, gerr := r.store.Get(ctx, e.ID)
			if gerr != nil {
				return RevisionView{}, gerr
			}
			view.PlanName, view.Components = planSummary(body)
		}
	}
	if !sawRevision {
		return RevisionView{}, fmt.Errorf("objmodel: %w: no %s in %s", ErrInvalid, fileRevision, root)
	}
	return view, nil
}

// resolveRevision turns a ref name or bare id into a revision tree root.
func (r *Reader) resolveRevision(ctx context.Context, ref string) (ObjectID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = refRevisionsLatest
	}
	if isObjectID(ref) {
		return objectstore.ObjectID(ref), nil
	}
	got, err := r.refs.Read(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("objmodel: revision %q: %w", ref, ErrNotFound)
	}
	return objectstore.ObjectID(got.Target), nil
}

// summaryOf projects an ExecutionView header into the lean listing shape.
func summaryOf(v ExecutionView) ExecSummary {
	return ExecSummary{
		ExecutionID:  v.ExecutionID,
		ExecutionKey: v.ExecutionKey,
		RevisionID:   v.RevisionID,
		TriggerID:    v.TriggerID,
		Status:       v.Status,
		StartedAt:    v.StartedAt,
		FinishedAt:   v.FinishedAt,
		Live:         v.Live,
		Summary:      v.Summary,
		ObjectID:     v.ObjectID,
	}
}

// planSummary extracts the plan's display name and the distinct component names
// it references, in plan order. Best-effort: an undecodable plan yields ("",
// nil) so callers degrade rather than fail (mirrors objread.PlanSummary).
func planSummary(body []byte) (name string, components []string) {
	var p struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Jobs []struct {
			Component string `json:"component"`
		} `json:"jobs"`
	}
	if json.Unmarshal(body, &p) != nil {
		return "", nil
	}
	seen := make(map[string]struct{}, len(p.Jobs))
	comps := make([]string, 0, len(p.Jobs))
	for _, j := range p.Jobs {
		if j.Component == "" {
			continue
		}
		if _, ok := seen[j.Component]; ok {
			continue
		}
		seen[j.Component] = struct{}{}
		comps = append(comps, j.Component)
	}
	if len(comps) == 0 {
		comps = nil
	}
	return p.Metadata.Name, comps
}

func decodeBlob[T any](ctx context.Context, s objectstore.ObjectStore, id ObjectID) (T, error) {
	var zero T
	_, body, err := s.Get(ctx, id)
	if err != nil {
		return zero, err
	}
	return nodes.Decode[T](body)
}

// isObjectID reports whether s looks like an "<algo>:<hex>" content id rather
// than a ref name. Ref names ("revisions/latest") contain a slash and no such
// prefix, so the check is unambiguous for the inputs the accessors accept.
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
