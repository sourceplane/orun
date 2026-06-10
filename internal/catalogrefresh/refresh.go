// Package catalogrefresh is the shared "resolve the workspace into the
// object-model catalog" engine. It is the single source of truth for the
// resolver inputs and the resolve+write step, so the CLI (`orun catalog
// refresh` / `orun plan`) and the cockpit (TUI) produce the SAME
// content-addressed catalog id for the same workspace — a re-resolve of an
// unchanged tree is an idempotent no-op, never churn.
//
// EnsureFresh adds the cockpit-side pieces (orun-legacy-retirement Bucket 6): a
// cheap source-hash staleness gate (skip the expensive resolve when the catalog
// already matches the current tree) and a non-blocking advisory try-lock (so a
// concurrent CLI refresh and the TUI don't both resolve). Data integrity is
// already guaranteed by the refstore lockfile + atomic ref rename; the try-lock
// only avoids wasted work.
package catalogrefresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// ResolverVersion is the integer resolver version stamped on resolver inputs
// (identity-and-keys.md §9). It must match the value the CLI uses so CLI/TUI
// catalogs share a content id.
//
// Bumped 1→2 for the orun-service-catalog SC1 envelope reshape (the resolved
// blob graduates from the flat manifest to the entity envelope), 2→3 for SC2
// (the catalog tree gains the single typed relations.json graph), and 3→4 for
// SC3 (the catalog tree gains the entities/<Kind>/ subtree + catalog.json
// countsByKind), and 4→5 for SC4 (component env bindings emit deployedTo edges
// + derived Environment entities). Every catalog id moves once per bump, the
// resolve memo misses once, and content addressing re-stabilizes (S-1).
const ResolverVersion = 5

// lockTTL bounds how long a refresh lock is honored before it is treated as
// stale (a crashed holder) and reclaimed. A resolve+write is sub-second; this
// is generous headroom.
const lockTTL = 2 * time.Minute

// Config carries the version stamps for a resolve. OrunVersion is cosmetic for
// the object-model catalog id (it is not embedded in the catalog node); only
// ResolverVersion + the resolved component/graph content drive the id.
type Config struct {
	OrunVersion string
	Strict      bool
}

// Result reports what EnsureFresh did. Exactly one of Fresh / Refreshed /
// Skipped is meaningful per call (plus Reused when Refreshed).
type Result struct {
	Fresh     bool   // catalog already matched the current source — no resolve ran
	Refreshed bool   // a resolve + object-model write ran
	Reused    bool   // the resolve produced the catalog id catalogs/current already held
	Skipped   bool   // another refresh held the lock — this call did nothing
	CatalogID string // the object-model catalog id (when known)
	SourceID  string // the current source snapshot content id
}

// EnsureFresh refreshes the object-model catalog under objModelRoot (the
// .orun/objectmodel directory) from the workspace at workspaceRoot, gated on a
// source-hash staleness check unless force is set.
//
//   - When the catalog already matches the current source and !force, it returns
//     {Fresh:true} without resolving.
//   - Otherwise it resolves the workspace (catalogresolve.BuildCatalog) and
//     writes the object-model catalog (objplan.RefreshCatalog) under a
//     non-blocking try-lock; a concurrent holder yields {Skipped:true}.
func EnsureFresh(ctx context.Context, objModelRoot, workspaceRoot string, force bool, cfg Config) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: objModelRoot})
	if err != nil {
		return Result{}, fmt.Errorf("open object store: %w", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: objModelRoot, Writer: "refresh"})
	if err != nil {
		return Result{}, fmt.Errorf("open ref store: %w", err)
	}

	// Probe the current source content id (cheap git probe + hash).
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return Result{}, fmt.Errorf("resolve source snapshot: %w", err)
	}
	curSrcID, err := sourceID(store, ws)
	if err != nil {
		return Result{}, err
	}

	// Staleness gate: when the stored catalog was resolved against the current
	// source, it is fresh — skip the expensive resolve (unless forced).
	if !force {
		if cat, lerr := objcatalog.New(store, refs).Load(ctx, "catalogs/current"); lerr == nil && cat.SourceID == curSrcID {
			return Result{Fresh: true, SourceID: curSrcID, CatalogID: string(cat.ObjectID)}, nil
		}
	}

	// Non-blocking try-lock: a concurrent refresh (CLI or TUI) wins, this skips.
	release, acquired, err := tryLock(objModelRoot)
	if err != nil {
		return Result{}, err
	}
	if !acquired {
		return Result{Skipped: true, SourceID: curSrcID}, nil
	}
	defer release()

	view, err := resolveWorkspace(ctx, workspaceRoot, ws, cfg)
	if err != nil {
		return Result{}, err
	}

	priorCatalog := ""
	if r, rerr := refs.Read(ctx, "catalogs/current"); rerr == nil {
		priorCatalog = r.Target
	}

	w := nodewriter.New(store, refs)
	memo := objplan.NewResolveMemo(objModelRoot)
	res, err := objplan.RefreshCatalog(ctx, w, store, memo, objplan.Input{
		Workspace:      ws,
		SourceHumanKey: sourcectx.BuildSourceSnapshotKey(ws),
		Resolve:        func() (*catalogresolve.CatalogView, error) { return view, nil },
	}, objplan.Options{Strict: cfg.Strict, OwnerResolver: objplan.OwnerResolverForWorkspace(workspaceRoot)})
	if err != nil {
		return Result{}, fmt.Errorf("write object-model catalog: %w", err)
	}

	return Result{
		Refreshed: true,
		Reused:    priorCatalog != "" && priorCatalog == string(res.CatalogID),
		CatalogID: string(res.CatalogID),
		SourceID:  string(res.SourceID),
	}, nil
}

// IsStale reports whether the object-model catalog at catalogs/current was
// resolved against a different source than the workspace currently has (so a
// refresh would change it). A missing/unreadable catalog counts as stale.
// Cheap: one git probe + hash, no resolve.
func IsStale(ctx context.Context, objModelRoot, workspaceRoot string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: objModelRoot})
	if err != nil {
		return true, err
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: objModelRoot, Writer: "refresh"})
	if err != nil {
		return true, err
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return true, err
	}
	curSrcID, err := sourceID(store, ws)
	if err != nil {
		return true, err
	}
	cat, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return true, nil
	}
	return cat.SourceID != curSrcID, nil
}

// sourceID returns the content id of the source snapshot for ws — the same id
// objplan.RefreshCatalog will write and that catalogs/current records.
func sourceID(store *objectstore.LocalStore, ws sourcectx.WorkspaceState) (string, error) {
	src := objplan.BuildSourceNode(ws, sourcectx.BuildSourceSnapshotKey(ws))
	id, err := nodes.SourceID(store.Algo(), src)
	if err != nil {
		return "", fmt.Errorf("compute source id: %w", err)
	}
	return string(id), nil
}

// resolveWorkspace runs the pure resolver over the workspace, producing the
// CatalogView objplan.RefreshCatalog writes. This is the single source of truth
// for the resolver inputs (shared with the CLI via the cmd/orun wrappers).
func resolveWorkspace(ctx context.Context, workspaceRoot string, ws sourcectx.WorkspaceState, cfg Config) (*catalogresolve.CatalogView, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	srcKey := sourcectx.BuildSourceSnapshotKey(ws)
	inputHash := CatalogInputHash(ws, cfg.OrunVersion)
	repo := RepoForInputs(ws.Repo, workspaceRoot)
	shortRepo := ShortRepoName(ws.Repo, workspaceRoot)
	inputs := ResolverInputsFromState(ws, srcKey, inputHash, repo, createdAt, cfg.OrunVersion)

	view, issues, err := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{
		WorkspaceRoot: workspaceRoot,
		Strict:        cfg.Strict,
		Repo:          shortRepo,
	}, inputs)
	if err != nil {
		return nil, fmt.Errorf("build catalog: %w", err)
	}
	if view == nil || view.Snapshot == nil {
		return nil, errors.New("build catalog: resolver returned no snapshot")
	}
	if cfg.Strict && len(issues) > 0 {
		return nil, fmt.Errorf("catalog has %d validation issue(s) under --strict", len(issues))
	}
	return view, nil
}

// tryLock acquires a non-blocking advisory lock (an O_EXCL lockfile) under
// objModelRoot. acquired=false means another refresh holds it. A lock older
// than lockTTL is treated as stale (crashed holder) and reclaimed once.
func tryLock(objModelRoot string) (release func(), acquired bool, err error) {
	if err := os.MkdirAll(objModelRoot, 0o755); err != nil {
		return nil, false, fmt.Errorf("ensure object-model root: %w", err)
	}
	path := filepath.Join(objModelRoot, "refresh.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, oerr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if oerr == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, true, nil
		}
		if !os.IsExist(oerr) {
			return nil, false, fmt.Errorf("acquire refresh lock: %w", oerr)
		}
		// Held — reclaim if stale, else report not-acquired.
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockTTL {
			_ = os.Remove(path)
			continue
		}
		return nil, false, nil
	}
	return nil, false, nil
}

// ---- resolver-input builders (single source of truth, shared with cmd/orun) --

// ShortRepoName returns the single-segment repo name used for componentKey
// construction (`<namespace>/<repo>/<name>`). A multi-segment "owner/repo" is
// reduced to its last segment; empty input falls back to the workspace base.
func ShortRepoName(wsRepo, workspaceRoot string) string {
	repo := wsRepo
	if repo == "" {
		repo = filepath.Base(workspaceRoot)
	}
	if idx := lastSlash(repo); idx >= 0 {
		repo = repo[idx+1:]
	}
	return repo
}

// repoForInputs returns the human-readable "<owner>/<repo>" repo string for
// ResolverInputs.Repo, falling back to the workspace base name.
func RepoForInputs(wsRepo, workspaceRoot string) string {
	if wsRepo != "" {
		return wsRepo
	}
	return filepath.Base(workspaceRoot)
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// CatalogInputHash computes the §8 catalogInputHash for the workspace.
func CatalogInputHash(ws sourcectx.WorkspaceState, orunVersion string) string {
	return sourcectx.CatalogInputHash(sourcectx.CatalogInputHashInputs{
		TreeHash:        ws.TreeHash,
		DirtyHash:       ws.DirtyHash,
		OrunVersion:     orunVersion,
		ResolverVersion: ResolverVersion,
		SchemaVersion:   catalogmodel.APIVersionV1Alpha1,
		StackSources:    nil,
		IntentCanonical: nil,
	})
}

// WorkingTreeLabel maps the dirty bool to the catalogmodel enum.
func WorkingTreeLabel(dirty bool) string {
	if dirty {
		return catalogmodel.WorkingTreeDirty
	}
	return catalogmodel.WorkingTreeClean
}

// Authoritative applies the data-model.md §2 rule: authoritative = (scope ∈
// canonical branches) AND (working tree clean).
func Authoritative(scope string, dirty bool) bool {
	if dirty {
		return false
	}
	switch scope {
	case catalogmodel.SourceScopeBranchMain, catalogmodel.SourceScopeBranchProtected:
		return true
	default:
		return false
	}
}

// SourceSnapshotFromState assembles the persisted SourceSnapshot record from a
// resolved WorkspaceState.
func SourceSnapshotFromState(ws sourcectx.WorkspaceState, srcKey, inputHash, createdAt string) catalogmodel.SourceSnapshot {
	return catalogmodel.SourceSnapshot{
		APIVersion:        catalogmodel.APIVersionV1Alpha1,
		Kind:              catalogmodel.KindSourceSnapshot,
		SourceSnapshotKey: srcKey,
		SourceSnapshotID:  catalogmodel.NewSourceSnapshotID(),
		Repo:              ws.Repo,
		RemoteURL:         ws.RemoteURL,
		Ref:               ws.Ref,
		Branch:            ws.Branch,
		SourceScope:       ws.Scope(),
		HeadRevision:      ws.HeadRevision,
		TreeHash:          ws.TreeHash,
		WorkingTree:       WorkingTreeLabel(ws.Dirty),
		DirtyHash:         ws.DirtyHash,
		CatalogInputHash:  inputHash,
		CreatedAt:         createdAt,
	}
}

// ResolverInputsFromState assembles the caller-supplied ResolverInputs the pure
// resolver cannot invent.
func ResolverInputsFromState(ws sourcectx.WorkspaceState, srcKey, inputHash, repo, createdAt, orunVersion string) catalogresolve.ResolverInputs {
	scope := ws.Scope()
	authoritative := Authoritative(scope, ws.Dirty)
	return catalogresolve.ResolverInputs{
		OrunVersion:       orunVersion,
		SchemaVersion:     catalogmodel.APIVersionV1Alpha1,
		ResolverVersion:   ResolverVersion,
		StackSources:      []string{},
		SourceSnapshotKey: srcKey,
		CatalogInputHash:  inputHash,
		Repo:              repo,
		SourceScope:       scope,
		HeadRevision:      ws.HeadRevision,
		TreeHash:          ws.TreeHash,
		WorkingTree:       WorkingTreeLabel(ws.Dirty),
		Authoritative:     authoritative,
		Preview:           !authoritative,
		CreatedAt:         createdAt,
	}
}
