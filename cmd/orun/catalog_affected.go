package main

// catalog_affected.go implements `orun catalog affected`: the internal/affected
// change-detection engine on the CLI. It reads the object-model catalog (its
// ownership map + dependency graph), classifies a git change, and emits the
// affected component set with a confidence signal — the same engine plan/run
// --changed and the cockpit overlay use (specs/orun-catalog-state/cli-surface.md
// §3).

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/objcatalog"
)

const kindCatalogAffectedResult = "CatalogAffectedResult"

var catalogAffectedJSON bool

// catalogAffectedData is the CatalogAffectedResult.data projection of
// affected.Result (cli-surface.md §3). Empty sets serialize as [] for a stable
// shape.
type catalogAffectedData struct {
	Affected         []string `json:"affected"`
	DirectlyChanged  []string `json:"directlyChanged"`
	Dependencies     []string `json:"dependencies"`
	Dependents       []string `json:"dependents"`
	Selection        []string `json:"selection"`
	Confidence       string   `json:"confidence"`
	NeedsFullResolve bool     `json:"needsFullResolve"`
	IntentMode       string   `json:"intentMode"`
	CatalogID        string   `json:"catalogId"`
}

func registerCatalogAffectedCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "affected",
		Short: "Compute the components affected by a change (the change-detection engine)",
		Long: `Compute the components affected by a change.

Reads the object-model catalog (its ownership map and dependency graph),
classifies the change between --base and --head (or an explicit --files list),
and reports the affected component set with a confidence signal. This is the
same engine 'orun plan/run --changed' and the cockpit use.

  directlyChanged  components whose own inputs changed
  dependents       components that transitively depend on a changed one
  affected         the cockpit "blast radius" (directlyChanged + dependents)
  selection        the plan/run job set (directlyChanged + include:always deps)

On classification ambiguity the engine over-reports (never under). A component.yaml
edit lowers confidence and sets needsFullResolve.

Examples:
  orun catalog affected
  orun catalog affected --base main --head HEAD
  orun catalog affected --files apps/api/main.go --json`,
		Args: cobra.NoArgs,
		RunE: runCatalogAffected,
	}
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base ref for change detection (default: main)")
	cmd.Flags().StringVar(&headRef, "head", "", "Head ref for change detection (default: working tree)")
	cmd.Flags().StringSliceVar(&changedFiles, "files", nil, "Comma-separated changed files (bypasses git diff)")
	cmd.Flags().StringVar(&intentImpact, "intent-impact", "watch", "How global intent changes affect components (all/watch/none)")
	cmd.Flags().BoolVar(&catalogAffectedJSON, "json", false, "Emit the CatalogAffectedResult JSON envelope")
	parent.AddCommand(cmd)
}

func runCatalogAffected(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	store, refs, _, err := openObjectModel()
	if err != nil {
		return exitErr(3, "open object model: %w", err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return exitErr(6, "no catalog found; run 'orun catalog refresh' or 'orun plan' first")
		}
		return exitErr(3, "load catalog: %w", err)
	}
	if view.Ownership == nil {
		return exitErr(6, "catalog has no impact index; run 'orun catalog refresh'")
	}

	opts := git.ChangeOptions{Base: baseBranch, Head: headRef, Files: changedFiles}
	if verr := git.ValidateOptions(opts); verr != nil {
		return exitErr(1, "invalid change options: %w", verr)
	}

	res, err := affected.NewDetector(&view, affected.IntentImpact(intentImpact)).
		Detect(ctx, affected.GitChangeSource{Options: opts, IntentPath: "intent.yaml"})
	if err != nil {
		return exitErr(2, "change detection: %w", err)
	}

	data := catalogAffectedData{
		Affected:         nonNilStrings(res.Affected),
		DirectlyChanged:  nonNilStrings(res.DirectlyChanged),
		Dependencies:     nonNilStrings(res.Dependencies),
		Dependents:       nonNilStrings(res.Dependents),
		Selection:        nonNilStrings(res.Selection),
		Confidence:       string(res.Confidence),
		NeedsFullResolve: res.NeedsFullResolve,
		IntentMode:       string(res.IntentMode),
		CatalogID:        string(view.ObjectID),
	}

	if catalogAffectedJSON {
		return writeCatalogEnvelope(kindCatalogAffectedResult, data, nil)
	}
	renderCatalogAffectedText(cmd.OutOrStdout(), data)
	return nil
}

// renderCatalogAffectedText prints a compact human-readable summary.
func renderCatalogAffectedText(w io.Writer, d catalogAffectedData) {
	fmt.Fprintf(w, "intent: %s   confidence: %s   needsFullResolve: %v\n", d.IntentMode, d.Confidence, d.NeedsFullResolve)
	printComponentList(w, "directly changed", d.DirectlyChanged)
	printComponentList(w, "dependents", d.Dependents)
	printComponentList(w, "affected (blast radius)", d.Affected)
	printComponentList(w, "selection (plan/run)", d.Selection)
}

func printComponentList(w io.Writer, label string, comps []string) {
	if len(comps) == 0 {
		fmt.Fprintf(w, "%s: (none)\n", label)
		return
	}
	fmt.Fprintf(w, "%s (%d):\n", label, len(comps))
	for _, c := range comps {
		fmt.Fprintf(w, "  - %s\n", c)
	}
}

// nonNilStrings returns a non-nil slice so JSON emits [] not null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
