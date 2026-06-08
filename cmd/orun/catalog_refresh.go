package main

// catalog_refresh.go implements `orun catalog refresh`: the write path that
// resolves the current workspace into a (SourceSnapshot, CatalogSnapshot) pair
// and persists it to the content-addressed object graph (the legacy catalogstore
// write was retired — specs/orun-legacy-retirement Bucket 1).
//
// Pipeline:
//
//	sourcectx.ResolveSourceSnapshot   → WorkspaceState
//	→ build ResolverInputs (catalog.go helpers)
//	→ catalogresolve.BuildCatalog      → CatalogView (pure)
//	→ writeObjectModelRefresh          → object-model source + catalog + refs
//
// Idempotency: a re-refresh of an unchanged workspace resolves to the same
// content-addressed catalog id (catalogs/current is unmoved), reported as the §2
// reuse form (exit 0).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/sourcectx"
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
	// ObjectModel is present when the object-model catalog write succeeded
	// (orun-catalog-state §2). Optional; existing keys stay byte-stable.
	ObjectModel *objectModelRefresh `json:"objectModel,omitempty"`
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

	parent.AddCommand(cmd)
}

func runCatalogRefresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// --sync no longer short-circuits: the local refresh always runs first,
	// then (when --sync is set) the configured syncer is invoked at the tail
	// of this function. In Phase 2 that syncer is catalogsync.NoopSyncer,
	// which performs no networking and reports the not-configured warning.

	// Validate any selector flags through the shared parser. Refresh always
	// resolves the *current* workspace, so the selector is not used to pick
	// a snapshot here — but a malformed --source/--catalog-snapshot must
	// still fail fast with the §2 exit-1 contract rather than be silently
	// ignored.
	if catalogSourceFlag != "" || catalogSnapshotFlag != "" {
		if _, err := objCatalogRef(catalogSourceFlag, catalogSnapshotFlag); err != nil {
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

	// The catalog is persisted to the object graph only (the legacy catalogstore
	// write was retired). created/reused is derived from the object-model write
	// in finishCatalogRefresh — a content-addressed catalog id unchanged from
	// catalogs/current is an idempotent reuse.
	data := catalogRefreshData{
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
	}
	return finishCatalogRefresh(ctx, data, view, ws)
}

// finishCatalogRefresh is the shared tail for both the created and reused
// paths. The local refresh has already succeeded; it writes the object-model
// catalog (orun-catalog-state §2) — populating the cockpit's source + the
// change-detection impact index from the same resolved view — and emits the
// result. The object-model write is best-effort: a failure is a warning, never
// a non-zero exit.
func finishCatalogRefresh(ctx context.Context, data catalogRefreshData, view *catalogresolve.CatalogView, ws sourcectx.WorkspaceState) error {
	var warnings []string
	if om, err := writeObjectModelRefresh(ctx, view, ws, data.SourceSnapshotKey); err != nil {
		warnings = append(warnings, fmt.Sprintf("object-model catalog write failed: %v", err))
		data.Created = true // write status unknown — report created rather than a false reuse
	} else {
		data.ObjectModel = &om
		data.Reused = om.Reused
		data.Created = !om.Reused
	}

	return emitRefreshResult(data, warnings)
}

// emitRefreshResult renders either the JSON envelope or the §2 text form.
// warnings carries any best-effort failures (e.g. the object-model write) that
// must surface without changing the exit code.
func emitRefreshResult(d catalogRefreshData, warnings []string) error {
	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogRefreshResult, d, warnings)
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
		renderRefreshWarnings(warnings)
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
	renderRefreshWarnings(warnings)
	return nil
}

// renderRefreshWarnings prints best-effort failures (text mode) under the
// summary. No-op when there are none.
func renderRefreshWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stdout, "  ⚠  %s\n", w)
	}
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

