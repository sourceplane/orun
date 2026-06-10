package main

// catalog_list.go implements `orun catalog list`: enumerate the components in
// the selected catalog with the cli-surface.md §3 columns
// (COMPONENT, TYPE, OWNER, SYSTEM, LAST EXEC, STATUS) and the
// --owner/--system/--domain/--type/--status filters.
//
// Data source: the object-model catalog (objcatalog) for the component rows —
// type/owner/system/domain from each component view — and the object-model
// execution history (objread.ComponentExecutions scan+filter join) for the
// lastExecution*/STATUS columns. Rows follow the catalog view's component order
// (sorted by component key) for byte-stable output. profile/environment are not
// part of the list row (the object-model execution does not record them).

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalog list filter flag values. Package scope so the cobra bindings and
// the RunE body share them; reset per-invocation by cobra.
var (
	catalogListOwnerFlag  string
	catalogListSystemFlag string
	catalogListDomainFlag string
	catalogListTypeFlag   string
	catalogListStatusFlag string
)

// catalogListRow is one row of the CatalogListResult envelope `data` array.
// Field names are the stable §3 JSON contract.
type catalogListRow struct {
	ComponentKey        string `json:"componentKey"`
	Name                string `json:"name"`
	Type                string `json:"type"`
	Owner               string `json:"owner"`
	System              string `json:"system"`
	LastRevisionKey     string `json:"lastRevisionKey"`
	LastExecutionKey    string `json:"lastExecutionKey"`
	LastExecutionStatus string `json:"lastExecutionStatus"`
	SourceSnapshotKey   string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey  string `json:"catalogSnapshotKey"`
}

func registerCatalogListCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the components in the selected catalog",
		Long: `List the components in the selected catalog.

Resolves the catalog via the shared source selector (default 'current') and
prints one row per component with its type, owner, system, and last execution
status. The filter flags narrow the set; output is sorted by component key.

Examples:
  orun catalog list
  orun catalog list --source main
  orun catalog list --owner team/platform-edge
  orun catalog list --type cloudflare-worker
  orun catalog list --json

Exit codes:
  0  Listing rendered (possibly empty).
  1  Invalid selector.
  3  StateStore failure.
  6  Catalog not found.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogList(cmd.Context())
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().StringVar(&catalogListOwnerFlag, "owner", "", "Only components with this owner")
	cmd.Flags().StringVar(&catalogListSystemFlag, "system", "", "Only components in this system")
	cmd.Flags().StringVar(&catalogListDomainFlag, "domain", "", "Only components in this domain")
	cmd.Flags().StringVar(&catalogListTypeFlag, "type", "", "Only components of this type")
	cmd.Flags().StringVar(&catalogListStatusFlag, "status", "", "Only components whose last execution has this status")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogList(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	view, reader, err := loadObjCatalog(ctx)
	if err != nil {
		return err
	}

	srcKey := view.SourceID
	catKey := objCatalogSnapshotKey(view)

	rows := make([]catalogListRow, 0, len(view.Components))
	for _, c := range view.Components {
		row := catalogListRow{
			ComponentKey:       c.ComponentKey,
			Name:               c.Name,
			Type:               c.Type,
			Owner:              c.Owner, // projected from the ownership block (SC1 reshape)
			System:             c.System,
			SourceSnapshotKey:  srcKey,
			CatalogSnapshotKey: catKey,
		}
		// lastExecution = newest execution that included this component (the
		// object-model scan+filter join; profile/environment are not part of
		// the list row).
		if execs, err := reader.ComponentExecutions(ctx, c.Name); err == nil && len(execs) > 0 {
			head := execs[0]
			row.LastRevisionKey = head.RevisionID
			row.LastExecutionKey = head.ExecutionKey
			row.LastExecutionStatus = head.Status
		}
		if !catalogListRowMatches(row, c) {
			continue
		}
		rows = append(rows, row)
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogListResult, rows, nil)
	}
	return renderCatalogListText(rows)
}

// catalogListRowMatches applies the §3 filter flags. domain is matched against
// the component's spec.domain (not surfaced as a column but a valid filter);
// the other axes match the rendered row fields.
func catalogListRowMatches(row catalogListRow, c objcatalog.CatalogComponentView) bool {
	if catalogListOwnerFlag != "" && row.Owner != catalogListOwnerFlag {
		return false
	}
	if catalogListSystemFlag != "" && row.System != catalogListSystemFlag {
		return false
	}
	if catalogListTypeFlag != "" && row.Type != catalogListTypeFlag {
		return false
	}
	if catalogListDomainFlag != "" && c.Domain != catalogListDomainFlag {
		return false
	}
	if catalogListStatusFlag != "" && row.LastExecutionStatus != catalogListStatusFlag {
		return false
	}
	return true
}

func renderCatalogListText(rows []catalogListRow) error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "No components in the selected catalog.")
		return nil
	}
	fmt.Fprintf(os.Stdout, "%s\n\n", ui.Bold(color, "Catalog components"))
	fmt.Fprintf(os.Stdout, "%-28s %-22s %-22s %-16s %-24s %s\n",
		"COMPONENT", "TYPE", "OWNER", "SYSTEM", "LAST EXEC", "STATUS")
	for _, r := range rows {
		lastExec := r.LastExecutionKey
		if lastExec == "" {
			lastExec = "-"
		}
		status := r.LastExecutionStatus
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(os.Stdout, "%-28s %-22s %-22s %-16s %-24s %s\n",
			truncField(r.Name, 28), truncField(r.Type, 22), truncField(r.Owner, 22),
			truncField(r.System, 16), truncField(lastExec, 24), status)
	}
	return nil
}

// truncField clamps a column value so the fixed-width table stays aligned for
// pathologically long values. Values within width are returned unchanged.
func truncField(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}
