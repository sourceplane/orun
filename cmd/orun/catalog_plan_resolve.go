package main

// catalog_plan_resolve.go bridges the C5 catalog write pipeline into the
// `orun plan` path (design.md §7, implementation-plan.md C6). Before a plan
// revision is persisted, the plan command resolves the current workspace into
// a (SourceSnapshot, CatalogSnapshot) pair and ensures it is persisted, so the
// revision can be mirrored under
// sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/.
//
// Design posture:
//   - The resolve is *best-effort* by default. A workspace with no resolvable
//     catalog (no git, resolver error, empty catalog) must NOT break the
//     Phase 1 plan flow — the plan still writes under the global layout and
//     the catalog-parent mirror is simply skipped. The hard-fail behaviour is
//     opt-in via `--catalog-strict` (wired in PR2).
//   - The heavy lifting (sourcectx → catalogresolve → catalogstore) is the
//     exact pipeline `orun catalog refresh` uses; this file only orchestrates
//     it and folds the result into a plan-friendly struct. No hashing or key
//     derivation happens here — those live in the engine packages.
//
// PR1 consumes only resolvePlanCatalog().Parent (the revision repath). PR2
// consumes the Source/Catalog provenance fields to populate
// plan.metadata.source / plan.metadata.catalog and honours the flags.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// planCatalogResolution is the result of resolving + persisting the catalog
// for a plan invocation. When Resolved is false the caller writes the plan
// under the Phase 1 layout only (Parent is the zero value, which
// revision.Config treats as inactive).
type planCatalogResolution struct {
	// Resolved is true when a (source, catalog) snapshot pair was resolved
	// and persisted. False means "fall back to Phase 1 layout".
	Resolved bool

	// Skipped is true when catalog resolution was deliberately bypassed
	// (`--no-catalog-refresh`). Distinct from Resolved=false-due-to-error:
	// PR2 surfaces this as metadata.catalog.skipped = true.
	Skipped bool

	// Parent is the catalog-parent ref threaded into revision.Config so the
	// revision body is mirrored under the catalog snapshot. Zero value when
	// !Resolved.
	Parent revision.CatalogParentRef

	// Source / Catalog carry the resolved snapshot records for PR2 metadata
	// population. Nil when !Resolved.
	Source  *catalogmodel.SourceSnapshot
	Catalog *catalogmodel.CatalogSnapshot

	// View is the full resolved catalog (manifests + graphs), threaded out so
	// the object-model plan hook (M5b) can persist the content-addressed catalog
	// without re-resolving. Nil on the existing-catalog / skip paths.
	View *catalogresolve.CatalogView
}

// planCatalogOptions controls resolvePlanCatalog. All fields default to the
// Phase-1-safe behaviour (best-effort, no skip, no strict) so a zero value is
// the conservative choice.
type planCatalogOptions struct {
	// NoRefresh short-circuits resolution entirely (`--no-catalog-refresh`).
	NoRefresh bool

	// Strict promotes a resolution/validation failure from a silent skip to
	// a hard error (`--catalog-strict`).
	Strict bool

	// SourceSelector, when non-empty, resolves an existing source/catalog
	// ref instead of building a fresh snapshot (`--catalog-source`).
	SourceSelector string

	// SnapshotKey, when non-empty, pins directly to an existing catalog
	// snapshot key instead of refreshing (`--catalog-snapshot`).
	SnapshotKey string
}

// resolvePlanCatalog resolves the current workspace into a catalog snapshot,
// persists it (idempotently — a byte-identical snapshot is reused), and
// returns the parent ref plus provenance. On any non-strict failure it
// returns a zero-Resolved result with no error so the plan flow continues
// under the Phase 1 layout.
func resolvePlanCatalog(ctx context.Context, opts planCatalogOptions) (planCatalogResolution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.NoRefresh {
		return planCatalogResolution{Skipped: true}, nil
	}

	if opts.SourceSelector != "" || opts.SnapshotKey != "" {
		res, err := resolveExistingCatalog(ctx, opts.SourceSelector, opts.SnapshotKey)
		if err != nil {
			if opts.Strict {
				return planCatalogResolution{}, err
			}
			return planCatalogResolution{}, nil
		}
		return res, nil
	}

	res, err := resolveAndPersistPlanCatalog(ctx, opts.Strict)
	if err != nil {
		if opts.Strict {
			return planCatalogResolution{}, err
		}
		return planCatalogResolution{}, nil
	}
	return res, nil
}

// resolveExistingCatalog resolves a (source, catalog) pair from
// already-persisted snapshots (--catalog-source / --catalog-snapshot).
// No refresh is performed; the snapshots must already exist in the state store.
func resolveExistingCatalog(ctx context.Context, sourceSelector, snapshotKey string) (planCatalogResolution, error) {
	stateStore, _, err := openLocalStateStore()
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("open state store: %w", err)
	}
	store := catalogstore.New(stateStore)

	sel, err := catalogstore.ParseRefSelector(sourceSelector, snapshotKey)
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("parse catalog selector: %w", err)
	}

	cat, err := store.ResolveCatalog(ctx, sel)
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("resolve catalog: %w", err)
	}

	src, err := store.ResolveSource(ctx, catalogstore.RefSelector{Kind: "current"})
	if err != nil {
		src = catalogmodel.SourceSnapshot{SourceSnapshotKey: cat.SourceSnapshotKey}
	}

	parent := revision.CatalogParentRef{
		SourceKey:  cat.SourceSnapshotKey,
		CatalogKey: cat.CatalogSnapshotKey,
	}

	return planCatalogResolution{
		Resolved: true,
		Parent:   parent,
		Source:   &src,
		Catalog:  &cat,
	}, nil
}

// resolveAndPersistPlanCatalog runs the refresh pipeline and persists the
// snapshot bundle, returning the resolution. Separated from resolvePlanCatalog
// so the best-effort/strict policy lives in one place and the pipeline stays
// linear and testable.
func resolveAndPersistPlanCatalog(ctx context.Context, strict bool) (planCatalogResolution, error) {
	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	// Stage 1 — resolve VCS context.
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{
		WorkspacePath: workspaceRoot,
	})
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("resolve source snapshot: %w", err)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	srcKey := sourcectx.BuildSourceSnapshotKey(ws)
	inputHash := buildCatalogInputHash(ws)
	repo := repoForInputs(ws.Repo, workspaceRoot)
	shortRepo := shortRepoName(ws.Repo, workspaceRoot)

	src := sourceSnapshotFromState(ws, srcKey, inputHash, createdAt)
	inputs := resolverInputsFromState(ws, srcKey, inputHash, repo, createdAt)

	// Stage 2 — pure resolve + build.
	view, issues, err := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{
		WorkspaceRoot: workspaceRoot,
		Strict:        strict,
		Repo:          shortRepo,
	}, inputs)
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("build catalog: %w", err)
	}
	if view == nil || view.Snapshot == nil {
		return planCatalogResolution{}, errors.New("build catalog: resolver returned no snapshot")
	}
	if strict && hasAnyIssue(issues) {
		return planCatalogResolution{}, fmt.Errorf("catalog has %d validation issue(s) under --catalog-strict", len(issues))
	}
	cat := view.Snapshot

	// Open stores.
	stateStore, _, err := openLocalStateStore()
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("open state store: %w", err)
	}
	store := catalogstore.New(stateStore)

	parent := revision.CatalogParentRef{
		SourceKey:  srcKey,
		CatalogKey: cat.CatalogSnapshotKey,
	}

	// Idempotency probe: if the catalog doc already exists, reuse it (the
	// snapshot bodies are immutable). Still refresh refs so a moved pointer
	// converges, mirroring `orun catalog refresh`.
	catPath, perr := catalogstore.CatalogDocPath(srcKey, cat.CatalogSnapshotKey)
	if perr != nil {
		return planCatalogResolution{}, fmt.Errorf("catalog doc path: %w", perr)
	}
	exists, perr := objectExists(ctx, stateStore, catPath)
	if perr != nil {
		return planCatalogResolution{}, fmt.Errorf("probe catalog doc: %w", perr)
	}
	if exists {
		if rerr := persistRefsOnly(ctx, store, src, *cat, ws, createdAt); rerr != nil {
			return planCatalogResolution{}, rerr
		}
		return planCatalogResolution{
			Resolved: true,
			Parent:   parent,
			Source:   &src,
			Catalog:  cat,
			View:     view,
		}, nil
	}

	// Stage 3 — assemble bundle.
	bundle, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:    src,
		Snapshot:  cat,
		Manifests: view.Manifests,
		Graphs:    view.Graphs,
		Branch:    refreshBranchScope(ws),
		PR:        refreshPRScope(ws),
		UpdatedAt: createdAt,
	})
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("assemble bundle: %w", err)
	}

	// Stage 4 — persist (source → catalog → global indexes → refs).
	if err := store.WriteSourceSnapshot(ctx, bundle.Source); err != nil {
		return planCatalogResolution{}, fmt.Errorf("write source snapshot: %w", err)
	}
	if err := store.WriteCatalogSnapshot(ctx, bundle.Source, bundle.Catalog, bundle.Manifests, bundle.Graphs, bundle.LocalIndexes); err != nil {
		return planCatalogResolution{}, fmt.Errorf("write catalog snapshot: %w", err)
	}
	if err := store.WriteGlobalIndexes(ctx, bundle.GlobalIndexes); err != nil {
		return planCatalogResolution{}, fmt.Errorf("write global indexes: %w", err)
	}
	if err := store.WriteRefs(ctx, bundle.Refs); err != nil {
		return planCatalogResolution{}, fmt.Errorf("write refs: %w", err)
	}

	return planCatalogResolution{
		Resolved: true,
		Parent:   parent,
		Source:   &src,
		Catalog:  cat,
		View:     view,
	}, nil
}
