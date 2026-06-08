package main

// catalog_plan_resolve.go bridges the catalog resolve pipeline into the
// `orun plan` path: before a plan is written, it resolves the current workspace
// into a (SourceSnapshot, CatalogSnapshot) pair so plan.metadata.source/catalog
// can be populated and the object-model plan can carry the catalog edge.
//
// Design posture:
//   - The resolve is best-effort by default. A workspace with no resolvable
//     catalog (no git, resolver error, empty catalog) must NOT break the plan
//     flow — the plan is written without catalog context. The hard-fail
//     behaviour is opt-in via `--catalog-strict`.
//   - The heavy lifting (sourcectx → catalogresolve) is the same pipeline
//     `orun catalog refresh` uses; this file orchestrates it and folds the
//     result into a plan-friendly struct. The resolved CatalogView is threaded
//     out so writeObjectModelPlan persists the content-addressed catalog without
//     re-resolving. No legacy persistence happens here (the catalogstore write
//     was retired — specs/orun-legacy-retirement Bucket 1).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// planCatalogResolution is the result of resolving the catalog for a plan
// invocation. When Resolved is false the plan is written without catalog
// context.
type planCatalogResolution struct {
	// Resolved is true when a (source, catalog) snapshot pair was resolved.
	Resolved bool

	// Skipped is true when catalog resolution was deliberately bypassed
	// (`--no-catalog-refresh`). Distinct from Resolved=false-due-to-error:
	// it surfaces as metadata.catalog.skipped = true.
	Skipped bool

	// Source / Catalog carry the resolved snapshot records for plan-metadata
	// population. Nil when !Resolved.
	Source  *catalogmodel.SourceSnapshot
	Catalog *catalogmodel.CatalogSnapshot

	// View is the full resolved catalog (manifests + graphs), threaded out so
	// the object-model plan hook persists the content-addressed catalog without
	// re-resolving. Nil on the skip path.
	View *catalogresolve.CatalogView
}

// planCatalogOptions controls resolvePlanCatalog. All fields default to the
// best-effort behaviour so a zero value is the conservative choice.
type planCatalogOptions struct {
	// NoRefresh short-circuits resolution entirely (`--no-catalog-refresh`).
	NoRefresh bool

	// Strict promotes a resolution/validation failure from a silent skip to
	// a hard error (`--catalog-strict`).
	Strict bool
}

// resolvePlanCatalog resolves the current workspace into a catalog snapshot and
// returns its provenance + the resolved view. On any non-strict failure it
// returns a zero-Resolved result with no error so the plan flow continues
// without catalog context.
func resolvePlanCatalog(ctx context.Context, opts planCatalogOptions) (planCatalogResolution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.NoRefresh {
		return planCatalogResolution{Skipped: true}, nil
	}

	res, err := resolveFreshPlanCatalog(ctx, opts.Strict)
	if err != nil {
		if opts.Strict {
			return planCatalogResolution{}, err
		}
		return planCatalogResolution{}, nil
	}
	return res, nil
}

// resolveFreshPlanCatalog resolves the workspace into a catalog snapshot via the
// shared catalogresolve pipeline and returns the provenance + view. It performs
// no persistence — the object-model plan hook (writeObjectModelPlan) writes the
// content-addressed catalog from the returned View.
func resolveFreshPlanCatalog(ctx context.Context, strict bool) (planCatalogResolution, error) {
	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return planCatalogResolution{}, fmt.Errorf("resolve workspace root: %w", err)
	}

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

	return planCatalogResolution{
		Resolved: true,
		Source:   &src,
		Catalog:  view.Snapshot,
		View:     view,
	}, nil
}
