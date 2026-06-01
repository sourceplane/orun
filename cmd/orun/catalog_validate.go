package main

// catalog_validate.go implements `orun catalog validate`: re-resolve the
// current workspace and report the typed validation result list from
// resolution-pipeline.md §6 + §8 (cli-surface.md §9). Exit 1 on any error;
// exit 0 with warnings unless --strict promotes them.
//
// Unlike `refresh`, validate is read-only: it runs catalogresolve.BuildCatalog
// against the live workspace and renders the issues without persisting. This
// matches the §9 contract — validate is a lint pass, not a writer.
//
// --rebuild-indexes (C8) reconstructs every global index file from the
// authoritative source tree via catalogstore.RebuildIndexes (catalog-store.md
// §8). The rebuild is byte-identical for the same input tree (T-STORE-3) and
// runs after the read/validate pass; a rebuild failure surfaces as exit 3.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var catalogValidateRebuildFlag bool

// catalogValidateIssue is the stable --json shape of one validation issue.
type catalogValidateIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	File     string `json:"file,omitempty"`
	Pointer  string `json:"pointer,omitempty"`
	Message  string `json:"message"`
}

// catalogValidateData is the CatalogValidateResult `data` payload.
type catalogValidateData struct {
	Valid    bool                   `json:"valid"`
	Errors   int                    `json:"errors"`
	Warnings int                    `json:"warnings"`
	Strict   bool                   `json:"strict"`
	Issues   []catalogValidateIssue `json:"issues"`
}

func registerCatalogValidateCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Re-resolve in strict mode and report validation issues",
		Long: `Re-resolve the current workspace and report the typed validation issues.

validate is read-only: it runs the resolver against the live workspace and
prints the §6/§8 validation result list without persisting a snapshot. Any
error fails with exit 1; warnings pass with exit 0 unless --strict promotes
them to errors.

Examples:
  orun catalog validate
  orun catalog validate --strict
  orun catalog validate --rebuild-indexes
  orun catalog validate --json

Exit codes:
  0  No errors (warnings allowed unless --strict).
  1  At least one validation error (or any warning under --strict).
  2  Resolver internal error.
  3  StateStore failure (including a global-index rebuild failure).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogValidate(cmd.Context())
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().BoolVar(&catalogStrictFlag, "catalog-strict", false, "Promote validation warnings to errors")
	cmd.Flags().BoolVar(&catalogStrictFlag, "strict", false, "Alias for --catalog-strict")
	cmd.Flags().BoolVar(&catalogValidateRebuildFlag, "rebuild-indexes", false, "Rebuild every global index from the source tree (catalog-store.md §8)")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogValidate(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// A malformed selector must still fail fast with the §2 exit-1 contract.
	if catalogSourceFlag != "" || catalogSnapshotFlag != "" {
		if _, err := parseCatalogSelector(); err != nil {
			return err
		}
	}

	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return exitErr(2, "%v", err)
	}

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

	_, issues, berr := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{
		WorkspaceRoot: workspaceRoot,
		Strict:        catalogStrictFlag,
		Repo:          shortRepo,
	}, inputs)
	if berr != nil {
		// A validation SeverityError surfaces through the error channel on a
		// strict abort. Fold it into the issue list rather than treating it
		// as an internal error so the §9 report is complete.
		var vi catalogresolve.ValidationIssue
		if errors.As(berr, &vi) {
			issues = appendIssueUnique(issues, vi)
		} else {
			return exitErr(2, "build catalog: %w", berr)
		}
	}

	data := summarizeValidation(issues)
	if catalogJSONFlag {
		if werr := writeCatalogEnvelope(kindCatalogValidateResult, data, nil); werr != nil {
			return werr
		}
	} else if rerr := renderCatalogValidateText(data); rerr != nil {
		return rerr
	}

	// --rebuild-indexes (C8): reconstruct every global index from the
	// authoritative source tree. Runs after the read/validate render so the
	// report is always emitted; a rebuild failure is a StateStore-class
	// failure (exit 3) and takes precedence over the validation-error exit.
	if catalogValidateRebuildFlag {
		if rerr := rebuildCatalogIndexes(ctx); rerr != nil {
			return rerr
		}
	}

	if data.Errors > 0 {
		return exitErr(1, "catalog validation failed: %d error(s)", data.Errors)
	}
	return nil
}

// rebuildCatalogIndexes opens the local state store and rebuilds every global
// index via catalogstore.RebuildIndexes (catalog-store.md §8). A clear status
// line is printed (suppressed in --json mode so the envelope stays the sole
// stdout payload). Failures surface as exit 3.
func rebuildCatalogIndexes(ctx context.Context) error {
	stateStore, _, err := openLocalStateStore()
	if err != nil {
		return exitErr(3, "open state store: %w", err)
	}
	store := catalogstore.New(stateStore)
	if err := store.RebuildIndexes(ctx); err != nil {
		return exitErr(3, "rebuild global indexes: %w", err)
	}
	if !catalogJSONFlag {
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "✓ Global indexes rebuilt"))
	}
	return nil
}

// appendIssueUnique adds vi to issues unless an identical (code, file, pointer)
// issue is already present (the strict abort issue is usually already in the
// returned list).
func appendIssueUnique(issues []catalogresolve.ValidationIssue, vi catalogresolve.ValidationIssue) []catalogresolve.ValidationIssue {
	for _, e := range issues {
		if e.Code == vi.Code && e.File == vi.File && e.Pointer == vi.Pointer {
			return issues
		}
	}
	return append(issues, vi)
}

func summarizeValidation(issues []catalogresolve.ValidationIssue) catalogValidateData {
	out := catalogValidateData{
		Strict: catalogStrictFlag,
		Issues: make([]catalogValidateIssue, 0, len(issues)),
	}
	for _, i := range issues {
		if i.Severity == catalogresolve.SeverityError {
			out.Errors++
		} else {
			out.Warnings++
		}
		out.Issues = append(out.Issues, catalogValidateIssue{
			Severity: i.Severity.String(),
			Code:     i.Code,
			File:     i.File,
			Pointer:  i.Pointer,
			Message:  i.Message,
		})
	}
	out.Valid = out.Errors == 0
	return out
}

func renderCatalogValidateText(d catalogValidateData) error {
	out := os.Stdout
	color := ui.ColorEnabledForWriter(out)
	if len(d.Issues) == 0 {
		fmt.Fprintf(out, "%s\n", ui.Bold(color, "✓ Catalog valid — no issues"))
		return nil
	}
	fmt.Fprintf(out, "%s\n\n", ui.Bold(color, "Validation issues"))
	for _, i := range d.Issues {
		loc := i.File
		if loc != "" && i.Pointer != "" {
			loc = loc + "#" + i.Pointer
		}
		if loc == "" {
			loc = "(repo-wide)"
		}
		fmt.Fprintf(out, "  %-7s %-32s %s — %s\n", i.Severity, i.Code, loc, i.Message)
	}
	fmt.Fprintf(out, "\n%d error(s), %d warning(s)\n", d.Errors, d.Warnings)
	return nil
}
