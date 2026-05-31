package main

// catalog_refresh.go implements `orun catalog refresh`: the write path that
// resolves the current workspace into a (SourceSnapshot, CatalogSnapshot)
// pair and persists it through internal/catalogstore. It is the only writer
// in the C5 catalog surface; every read subcommand consumes what this writes.
//
// Pipeline (task-0036 Objective 1):
//
//	sourcectx.ResolveSourceSnapshot   → WorkspaceState
//	→ build SourceSnapshot + ResolverInputs (catalog.go helpers)
//	→ catalogresolve.BuildCatalog      → CatalogView (pure)
//	→ catalogstore.AssembleBundle      → CatalogBundle (pure seam)
//	→ WriteSourceSnapshot / WriteCatalogSnapshot / WriteGlobalIndexes / WriteRefs
//
// Idempotency: a byte-identical re-refresh resolves to the same
// catalogSnapshotKey; the catalog doc already exists, so we print the §2
// reuse form and exit 0 without re-writing (and without minting a new
// snapshot directory).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalogRefreshData is the `data` payload of the CatalogRefreshResult
// envelope. Field names are part of the stable JSON contract.
type catalogRefreshData struct {
	Created            bool   `json:"created"`
	Reused             bool   `json:"reused"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	Ref                string `json:"ref"`
	TreeHash           string `json:"treeHash"`
	WorkingTree        string `json:"workingTree"`
	Mode               string `json:"mode"`
	Authoritative      bool   `json:"authoritative"`
	Dirty              bool   `json:"dirty"`
	Components         int    `json:"components"`
	Systems            int    `json:"systems"`
	APIs               int    `json:"apis"`
	Resources          int    `json:"resources"`
	Path               string `json:"path"`
}

func registerCatalogRefreshCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Resolve the current workspace and persist a catalog snapshot",
		Long: `Resolve the current workspace into a (SourceSnapshot, CatalogSnapshot)
pair and persist it through the catalog store.

A byte-identical re-refresh is idempotent: it reuses the existing snapshot,
prints the "up to date" form, and creates no new snapshot directory. When the
worktree is dirty the snapshot is marked local-only and a banner is printed.

Examples:
  orun catalog refresh
  orun catalog refresh --json

Exit codes:
  0  Snapshot created or reused (idempotent).
  1  Validation error (or any warning under --strict).
  2  Resolver internal error.
  3  StateStore conflict / persistence failure.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogRefresh(cmd.Context())
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().BoolVar(&catalogStrictFlag, "catalog-strict", false, "Promote validation warnings to errors")
	cmd.Flags().BoolVar(&catalogStrictFlag, "strict", false, "Alias for --catalog-strict")
	cmd.Flags().BoolVar(&catalogNoInferFlag, "no-infer", false, "Disable the inference layer (stage 6)")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")
	cmd.Flags().BoolVar(&catalogSyncFlag, "sync", false, "Phase 2: prints 'remote sync not configured' and exits 0")

	parent.AddCommand(cmd)
}

func runCatalogRefresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// --sync is a no-op in Phase 2 (no networking). Documented line, exit 0.
	if catalogSyncFlag {
		fmt.Fprintln(os.Stdout, "remote sync not configured")
		return nil
	}

	// Validate any selector flags through the shared parser. Refresh always
	// resolves the *current* workspace, so the selector is not used to pick
	// a snapshot here — but a malformed --source/--catalog-snapshot must
	// still fail fast with the §2 exit-1 contract rather than be silently
	// ignored.
	if catalogSourceFlag != "" || catalogSnapshotFlag != "" {
		if _, err := parseCatalogSelector(); err != nil {
			return err
		}
	}

	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return exitErr(2, "%v", err)
	}

	// Stage 1 — resolve the workspace VCS context.
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{
		WorkspacePath: workspaceRoot,
	})
	if err != nil {
		return exitErr(2, "resolve source snapshot: %w", err)
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
		Strict:        catalogStrictFlag,
		Repo:          shortRepo,
	}, inputs)
	if err != nil {
		// A validation SeverityError surfaces here. Distinguish a
		// validation abort (exit 1) from a resolver-internal bug (exit 2).
		if isValidationError(err) {
			return exitErr(1, "catalog validation failed: %w", err)
		}
		return exitErr(2, "build catalog: %w", err)
	}
	if view == nil || view.Snapshot == nil {
		return exitErr(2, "build catalog: resolver returned no snapshot")
	}

	// --strict: any warning is an error (the resolver already promotes
	// warnings to errors in strict mode, but guard defensively).
	if catalogStrictFlag && hasAnyIssue(issues) {
		return exitErr(1, "catalog has %d validation issue(s) under --strict", len(issues))
	}

	cat := view.Snapshot

	// Open the state store + catalog store.
	stateStore, _, err := openLocalStateStore()
	if err != nil {
		return exitErr(3, "open state store: %w", err)
	}
	store := catalogstore.New(stateStore)

	// Idempotency probe: if the catalog doc already exists, this is a
	// byte-identical re-refresh — print the reuse form and exit 0.
	catPath, perr := catalogstore.CatalogDocPath(srcKey, cat.CatalogSnapshotKey)
	if perr != nil {
		return exitErr(3, "catalog doc path: %w", perr)
	}
	exists, perr := objectExists(ctx, stateStore, catPath)
	if perr != nil {
		return exitErr(3, "probe catalog doc: %w", perr)
	}
	if exists {
		// Still refresh the refs so a moved ref pointer converges, but the
		// snapshot bodies are immutable and already present.
		if rerr := persistRefsOnly(ctx, store, src, *cat, ws, createdAt); rerr != nil {
			return rerr
		}
		return emitRefreshResult(catalogRefreshData{
			Reused:             true,
			SourceSnapshotKey:  srcKey,
			CatalogSnapshotKey: cat.CatalogSnapshotKey,
			Ref:                ws.Ref,
			TreeHash:           ws.TreeHash,
			WorkingTree:        workingTreeLabel(ws.Dirty),
			Mode:               modeLabel(cat.Authoritative),
			Authoritative:      cat.Authoritative,
			Dirty:              ws.Dirty,
			Components:         cat.Summary.Components,
			Systems:            cat.Summary.Systems,
			APIs:               cat.Summary.APIs,
			Resources:          cat.Summary.Resources,
			Path:               catPath,
		})
	}

	// Stage 3 — assemble the persistence bundle (pure seam).
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
		return exitErr(2, "assemble bundle: %w", err)
	}

	// Stage 4 — persist (source → catalog → global indexes → refs).
	if err := store.WriteSourceSnapshot(ctx, bundle.Source); err != nil {
		// A byte-divergent re-write at the same key is a real conflict.
		return exitErr(3, "write source snapshot: %w", err)
	}
	if err := store.WriteCatalogSnapshot(ctx, bundle.Source, bundle.Catalog, bundle.Manifests, bundle.Graphs, bundle.LocalIndexes); err != nil {
		if errors.Is(err, catalogstore.ErrInputsInconsistent) {
			return exitErr(2, "write catalog snapshot: %w", err)
		}
		return exitErr(3, "write catalog snapshot: %w", err)
	}
	if err := store.WriteGlobalIndexes(ctx, bundle.GlobalIndexes); err != nil {
		return exitErr(3, "write global indexes: %w", err)
	}
	if err := store.WriteRefs(ctx, bundle.Refs); err != nil {
		return exitErr(3, "write refs: %w", err)
	}

	return emitRefreshResult(catalogRefreshData{
		Created:            true,
		SourceSnapshotKey:  srcKey,
		CatalogSnapshotKey: cat.CatalogSnapshotKey,
		Ref:                ws.Ref,
		TreeHash:           ws.TreeHash,
		WorkingTree:        workingTreeLabel(ws.Dirty),
		Mode:               modeLabel(cat.Authoritative),
		Authoritative:      cat.Authoritative,
		Dirty:              ws.Dirty,
		Components:         cat.Summary.Components,
		Systems:            cat.Summary.Systems,
		APIs:               cat.Summary.APIs,
		Resources:          cat.Summary.Resources,
		Path:               catPath,
	})
}

// persistRefsOnly re-writes the ref pointers on an idempotent reuse so a
// moved ref converges to the current snapshot without re-writing immutable
// bodies. Failures here are persistence failures (exit 3).
func persistRefsOnly(ctx context.Context, store catalogstore.Store, src catalogmodel.SourceSnapshot, cat catalogmodel.CatalogSnapshot, ws sourcectx.WorkspaceState, updatedAt string) error {
	bundle, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:    src,
		Snapshot:  &cat,
		Branch:    refreshBranchScope(ws),
		PR:        refreshPRScope(ws),
		UpdatedAt: updatedAt,
	})
	if err != nil {
		return exitErr(2, "assemble refs bundle: %w", err)
	}
	if err := store.WriteRefs(ctx, bundle.Refs); err != nil {
		return exitErr(3, "write refs: %w", err)
	}
	return nil
}

// emitRefreshResult renders either the JSON envelope or the §2 text form.
func emitRefreshResult(d catalogRefreshData) error {
	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogRefreshResult, d, nil)
	}

	color := ui.ColorEnabledForWriter(os.Stdout)

	if d.Dirty {
		fmt.Fprintln(os.Stdout, "⚠  Dirty worktree: snapshot is local-only.")
		fmt.Fprintln(os.Stdout, "    Use --sync-dirty-preview when remote sync is configured (Phase 3).")
		fmt.Fprintln(os.Stdout)
	}

	if d.Reused {
		fmt.Fprintf(os.Stdout, "%s\n\n", ui.Bold(color, "↺ Catalog up to date"))
		fmt.Fprintf(os.Stdout, "Source:   %s\n", d.SourceSnapshotKey)
		fmt.Fprintf(os.Stdout, "Catalog:  %s\n", d.CatalogSnapshotKey)
		return nil
	}

	fmt.Fprintf(os.Stdout, "%s\n\n", ui.Bold(color, "✓ Catalog snapshot created"))
	fmt.Fprintf(os.Stdout, "Source:     %s\n", d.SourceSnapshotKey)
	fmt.Fprintf(os.Stdout, "Catalog:    %s\n", d.CatalogSnapshotKey)
	if d.Ref != "" {
		fmt.Fprintf(os.Stdout, "Ref:        %s\n", d.Ref)
	}
	if d.TreeHash != "" {
		fmt.Fprintf(os.Stdout, "Tree:       %s\n", d.TreeHash)
	}
	fmt.Fprintf(os.Stdout, "State:      %s\n", d.WorkingTree)
	fmt.Fprintf(os.Stdout, "Mode:       %s\n", d.Mode)
	fmt.Fprintf(os.Stdout, "Components: %d\n", d.Components)
	fmt.Fprintf(os.Stdout, "Systems:    %d\n", d.Systems)
	fmt.Fprintf(os.Stdout, "APIs:       %d\n", d.APIs)
	fmt.Fprintf(os.Stdout, "Resources:  %d\n", d.Resources)
	fmt.Fprintf(os.Stdout, "Path:       %s\n", d.Path)
	return nil
}

// modeLabel renders the authoritative flag as the §2 "Mode:" label.
func modeLabel(authoritative bool) string {
	if authoritative {
		return "authoritative"
	}
	return "preview"
}

// refreshBranchScope returns the branch name for a feature/protected-branch
// scope so the writer emits a refs/{sources,catalogs}/branches/<branch> ref.
// Empty for main / pr / local scopes (the canonical ref carries those).
func refreshBranchScope(ws sourcectx.WorkspaceState) string {
	switch ws.Scope() {
	case catalogmodel.SourceScopeBranchFeature, catalogmodel.SourceScopeBranchProtected:
		return ws.Branch
	default:
		return ""
	}
}

// refreshPRScope returns the decimal PR number for a pr scope so the writer
// emits a refs/{sources,catalogs}/prs/<pr> ref. Empty otherwise.
func refreshPRScope(ws sourcectx.WorkspaceState) string {
	if ws.Scope() == catalogmodel.SourceScopePR && ws.PRNumber > 0 {
		return fmt.Sprintf("%d", ws.PRNumber)
	}
	return ""
}

// isValidationError reports whether err is a resolver validation abort (a
// catalogresolve.ValidationIssue surfaced via the error channel) rather than
// an internal resolver bug. Validation aborts map to exit 1.
func isValidationError(err error) bool {
	var vi catalogresolve.ValidationIssue
	return errors.As(err, &vi)
}

// hasAnyIssue reports whether the resolver returned any validation issue.
func hasAnyIssue(issues []catalogresolve.ValidationIssue) bool {
	return len(issues) > 0
}

// ensure statestore import is used even if the error-path helpers above are
// compiled out by future refactors.
var _ = statestore.ErrNotFound
