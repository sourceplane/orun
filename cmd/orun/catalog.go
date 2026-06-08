package main

// catalog.go is the foundation for the `orun catalog` command family. It ships
// the root command, the shared JSON-envelope writer, the shared selector flags,
// and the pure helpers that turn a resolved workspace into the
// (SourceSnapshot, ResolverInputs) the resolver needs.
//
// Architecture: the CLI is glue only. Resolution is delegated to
// internal/sourcectx + internal/catalogresolve; persistence and reads go to the
// content-addressed object model (the legacy catalogstore/statestore have been
// retired — specs/orun-legacy-retirement). Selector parsing now routes through
// objCatalogRef (catalog_objsource.go).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/spf13/cobra"
)

// catalogResolverVersion is the integer resolver version the CLI stamps on
// every catalog it builds. It feeds catalogHash via ResolverInputs and the
// §8 catalogInputHash. Bumping it intentionally invalidates every cached
// catalogSnapshotKey (a resolver-behavior change must produce a new key).
const catalogResolverVersion = 1

// catalog refresh flag values. Declared at package scope so the cobra flag
// bindings and the RunE bodies share them; reset per-invocation by cobra.
var (
	catalogSourceFlag   string
	catalogSnapshotFlag string
	catalogStrictFlag   bool
	catalogNoInferFlag  bool
	catalogJSONFlag     bool
)

// catalogEnvelope is the Orun JSON envelope (cli-surface.md §11). The exact
// key names and casing are load-bearing — later C5 commands and external
// consumers depend on them byte-for-byte.
type catalogEnvelope struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Data       any      `json:"data"`
	Warnings   []string `json:"warnings"`
}

// Envelope kinds shipped by the C5 catalog surface (cli-surface.md §11).
const (
	kindCatalogRefreshResult  = "CatalogRefreshResult"
	kindCatalogRefsResult     = "CatalogRefsResult"
	kindCatalogListResult     = "CatalogListResult"
	kindCatalogDescribeResult = "CatalogDescribeResult"
	kindCatalogTreeResult     = "CatalogTreeResult"
	kindCatalogHistoryResult  = "CatalogHistoryResult"
	kindCatalogValidateResult = "CatalogValidateResult"
	kindCatalogDiffResult     = "CatalogDiffResult"
)

// writeCatalogEnvelope renders data as the standard envelope to stdout with
// two-space indentation. warnings is always emitted as a (possibly empty)
// array so the shape is stable for golden tests.
func writeCatalogEnvelope(kind string, data any, warnings []string) error {
	if warnings == nil {
		warnings = []string{}
	}
	env := catalogEnvelope{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       kind,
		Data:       data,
		Warnings:   warnings,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// catalogExitError carries a CLI exit code alongside the error message.
// main() checks for the ExitCode() method and uses it instead of the
// default exit-1. This keeps the §2 exit-code contract (0/1/2/3) testable
// without os.Exit inside the RunE.
type catalogExitError struct {
	code int
	err  error
}

func (e *catalogExitError) Error() string { return e.err.Error() }
func (e *catalogExitError) Unwrap() error { return e.err }
func (e *catalogExitError) ExitCode() int { return e.code }

// exitErr is a small constructor for catalogExitError from a format string.
func exitErr(code int, format string, args ...any) *catalogExitError {
	return &catalogExitError{code: code, err: fmt.Errorf(format, args...)}
}

// registerCatalogCommand wires `orun catalog` (and its subcommands) onto the
// CLI root. Additive: removing this call removes the entire command family
// with zero impact on existing commands.
func registerCatalogCommand(root *cobra.Command) {
	catalogCmd := &cobra.Command{
		Use:   "catalog",
		Short: "Resolve, persist, and inspect the component catalog",
		Long: `Resolve, persist, and inspect the component catalog.

The catalog is the resolved set of components (plus their graphs and indexes)
for a workspace at a given source snapshot. ` + "`orun catalog refresh`" + ` builds
and persists a snapshot; the read subcommands inspect what has been persisted.

Subcommands:
  refresh   Resolve the current workspace and persist a catalog snapshot
  list      List the components in the selected catalog
  describe  Show the full resolved manifest for one component
  tree      Render the catalog relationship graphs
  history   Enumerate a component's execution history
  validate  Re-resolve in strict mode and report validation issues
  diff      Compare two catalog snapshots
  refs      List every catalog ref with its resolved source/catalog keys
  affected  Compute the components affected by a change (change-detection engine)

Run 'orun catalog <subcommand> --help' for details on each.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registerCatalogRefreshCommand(catalogCmd)
	registerCatalogListCommand(catalogCmd)
	registerCatalogDescribeCommand(catalogCmd)
	registerCatalogTreeCommand(catalogCmd)
	registerCatalogHistoryCommand(catalogCmd)
	registerCatalogValidateCommand(catalogCmd)
	registerCatalogDiffCommand(catalogCmd)
	registerCatalogRefsCommand(catalogCmd)
	registerCatalogAffectedCommand(catalogCmd)

	root.AddCommand(catalogCmd)
}

// addCatalogSelectorFlags binds the shared selector flags onto a subcommand.
// `--source` is the short alias for `--catalog-source` (cli-surface.md §1).
func addCatalogSelectorFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&catalogSourceFlag, "catalog-source", "", "Resolve catalog by ref selector (current|main|latest|branches/<name>|prs/<n>|cat-<key>)")
	cmd.Flags().StringVar(&catalogSourceFlag, "source", "", "Alias for --catalog-source")
	cmd.Flags().StringVar(&catalogSnapshotFlag, "catalog-snapshot", "", "Bypass refs; pin to an explicit catalogSnapshotKey")
}

// ----- workspace → engine-input helpers (pure glue) ------------------

// catalogWorkspaceRoot returns the absolute workspace root the resolver
// walks. It mirrors the store-root convention: the intent root (repo root)
// when an intent file was discovered, else the current directory.
func catalogWorkspaceRoot() (string, error) {
	abs, err := filepath.Abs(storeDir())
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return abs, nil
}

// computeCatalogAuthoritative applies the data-model.md §2 rule:
// authoritative = (sourceScope ∈ canonical branches) AND (workingTree clean).
// Canonical branches are branch-main and branch-protected.
func computeCatalogAuthoritative(scope string, dirty bool) bool {
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

// repoForInputs returns a non-empty repo string for ResolverInputs.Repo,
// which BuildCatalog requires. This is the human-readable "<owner>/<repo>"
// form. Falls back to the workspace base name when the workspace has no git
// remote (local-nogit / no-origin clones).
func repoForInputs(wsRepo, workspaceRoot string) string {
	if wsRepo != "" {
		return wsRepo
	}
	return filepath.Base(workspaceRoot)
}

// shortRepoName returns the single-segment repo name used for componentKey
// construction (`<namespace>/<repo>/<name>`). The resolver requires this to
// be ONE path segment, so an "<owner>/<repo>" repo string is reduced to its
// last segment. Empty input falls back to the workspace base name.
func shortRepoName(wsRepo, workspaceRoot string) string {
	repo := wsRepo
	if repo == "" {
		repo = filepath.Base(workspaceRoot)
	}
	// Reduce "owner/repo" (or any multi-segment form) to the final segment.
	if idx := strings.LastIndex(repo, "/"); idx >= 0 {
		repo = repo[idx+1:]
	}
	return repo
}

// workingTreeLabel maps the dirty bool to the catalogmodel enum.
func workingTreeLabel(dirty bool) string {
	if dirty {
		return catalogmodel.WorkingTreeDirty
	}
	return catalogmodel.WorkingTreeClean
}

// buildCatalogInputHash computes the §8 catalogInputHash for the workspace.
// PR-1 narrow assumption: StackSources and the intent-canonical block are
// empty for the refresh path (composition-stack resolution is a later
// milestone). This keeps the hash deterministic and is documented in the
// task report; when stacks land they fold in here without a CLI-shape change.
func buildCatalogInputHash(ws sourcectx.WorkspaceState) string {
	return sourcectx.CatalogInputHash(sourcectx.CatalogInputHashInputs{
		TreeHash:        ws.TreeHash,
		DirtyHash:       ws.DirtyHash,
		OrunVersion:     version,
		ResolverVersion: catalogResolverVersion,
		SchemaVersion:   catalogmodel.APIVersionV1Alpha1,
		StackSources:    nil,
		IntentCanonical: nil,
	})
}

// sourceSnapshotFromState assembles the persisted SourceSnapshot record from
// a resolved WorkspaceState. The SourceSnapshotID is a fresh ULID; it is only
// written on first creation (the refresh path skips the source write when the
// doc already exists), so the non-deterministic ID never breaks idempotency.
func sourceSnapshotFromState(ws sourcectx.WorkspaceState, srcKey, inputHash, createdAt string) catalogmodel.SourceSnapshot {
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
		WorkingTree:       workingTreeLabel(ws.Dirty),
		DirtyHash:         ws.DirtyHash,
		CatalogInputHash:  inputHash,
		CreatedAt:         createdAt,
	}
}

// resolverInputsFromState assembles the caller-supplied ResolverInputs the
// pure resolver cannot invent.
func resolverInputsFromState(ws sourcectx.WorkspaceState, srcKey, inputHash, repo, createdAt string) catalogresolve.ResolverInputs {
	scope := ws.Scope()
	authoritative := computeCatalogAuthoritative(scope, ws.Dirty)
	return catalogresolve.ResolverInputs{
		OrunVersion:       version,
		SchemaVersion:     catalogmodel.APIVersionV1Alpha1,
		ResolverVersion:   catalogResolverVersion,
		StackSources:      []string{},
		SourceSnapshotKey: srcKey,
		CatalogInputHash:  inputHash,
		Repo:              repo,
		SourceScope:       scope,
		HeadRevision:      ws.HeadRevision,
		TreeHash:          ws.TreeHash,
		WorkingTree:       workingTreeLabel(ws.Dirty),
		Authoritative:     authoritative,
		Preview:           !authoritative,
		CreatedAt:         createdAt,
	}
}

