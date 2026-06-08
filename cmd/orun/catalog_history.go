package main

// catalog_history.go implements `orun catalog history <component>`: enumerate a
// component's execution history from the object-model execution graph
// (objread.ComponentExecutions scan+filter join), cli-surface.md §7. Columns:
// TIME, REVISION, EXEC, TRIGGER, PROFILE, ENV, STATUS — newest-first, limit 50.
//
// Filters: --trigger, --profile, --environment narrow the rows; --limit caps
// the count. A component that has never executed prints an empty list and exits
// 0. Note: the object-model execution does not record per-execution profile/
// environment/triggerName, so those columns are empty (a documented v1 gap of
// the catalogstore retirement; specs/orun-legacy-retirement Bucket 1).

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	catalogHistoryTriggerFlag string
	catalogHistoryProfileFlag string
	catalogHistoryEnvFlag     string
	catalogHistoryLimitFlag   int
)

func registerCatalogHistoryCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "history <component>",
		Short: "Enumerate a component's execution history",
		Long: `Enumerate a component's execution history from the catalog-local index.

Rows are sorted newest-first and capped at --limit (default 50). The trigger,
profile, and environment filters narrow the set. A component that has never
executed prints an empty history and exits 0.

Examples:
  orun catalog history api-edge
  orun catalog history api-edge --source main
  orun catalog history api-edge --trigger github-push-main
  orun catalog history api-edge --limit 10 --json

Exit codes:
  0  History rendered (possibly empty).
  1  Invalid selector or missing component argument.
  3  StateStore failure.
  6  Catalog not found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogHistory(cmd.Context(), args[0])
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().StringVar(&catalogHistoryTriggerFlag, "trigger", "", "Only executions from this trigger")
	cmd.Flags().StringVar(&catalogHistoryProfileFlag, "profile", "", "Only executions of this profile")
	cmd.Flags().StringVar(&catalogHistoryEnvFlag, "environment", "", "Only executions in this environment")
	cmd.Flags().IntVar(&catalogHistoryLimitFlag, "limit", 50, "Maximum number of rows")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogHistory(ctx context.Context, arg string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return exitErr(1, "history requires a component name")
	}
	if catalogHistoryLimitFlag < 0 {
		return exitErr(1, "--limit must be >= 0")
	}

	view, reader, err := loadObjCatalog(ctx)
	if err != nil {
		return err
	}

	// The object-model execution scan+filter join. executionKey/revision/status
	// come from the sealed execution; profile/environment/triggerName are not
	// recorded on the object-model execution, so they are emitted empty (a
	// documented v1 gap — see specs/orun-legacy-retirement Bucket 1).
	execs, err := reader.ComponentExecutions(ctx, arg)
	if err != nil {
		return exitErr(3, "read executions: %w", err)
	}
	srcKey := view.SourceID
	catKey := objCatalogSnapshotKey(view)
	compKey := objComponentKey(view, arg)
	rows := make([]catalogmodel.ComponentExecutionRow, 0, len(execs))
	for _, e := range execs {
		rows = append(rows, catalogmodel.ComponentExecutionRow{
			ComponentKey:       compKey,
			SourceSnapshotKey:  srcKey,
			CatalogSnapshotKey: catKey,
			RevisionKey:        e.RevisionID,
			ExecutionKey:       e.ExecutionKey,
			Status:             e.Status,
			CreatedAt:          e.StartedAt.UTC().Format(time.RFC3339),
		})
	}
	rows = filterHistoryRows(rows)
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].CreatedAt > rows[b].CreatedAt })
	if catalogHistoryLimitFlag > 0 && len(rows) > catalogHistoryLimitFlag {
		rows = rows[:catalogHistoryLimitFlag]
	}

	if catalogJSONFlag {
		if rows == nil {
			rows = []catalogmodel.ComponentExecutionRow{}
		}
		return writeCatalogEnvelope(kindCatalogHistoryResult, rows, nil)
	}
	return renderCatalogHistoryText(rows)
}

func filterHistoryRows(rows []catalogmodel.ComponentExecutionRow) []catalogmodel.ComponentExecutionRow {
	if catalogHistoryTriggerFlag == "" && catalogHistoryProfileFlag == "" && catalogHistoryEnvFlag == "" {
		return rows
	}
	var out []catalogmodel.ComponentExecutionRow
	for _, r := range rows {
		if catalogHistoryTriggerFlag != "" && r.TriggerName != catalogHistoryTriggerFlag {
			continue
		}
		if catalogHistoryProfileFlag != "" && r.Profile != catalogHistoryProfileFlag {
			continue
		}
		if catalogHistoryEnvFlag != "" && r.Environment != catalogHistoryEnvFlag {
			continue
		}
		out = append(out, r)
	}
	return out
}

func renderCatalogHistoryText(rows []catalogmodel.ComponentExecutionRow) error {
	out := os.Stdout
	color := ui.ColorEnabledForWriter(out)
	if len(rows) == 0 {
		fmt.Fprintln(out, "No executions recorded for this component.")
		return nil
	}
	fmt.Fprintf(out, "%s\n\n", ui.Bold(color, "Execution history"))
	fmt.Fprintf(out, "%-22s %-22s %-22s %-20s %-16s %-12s %s\n",
		"TIME", "REVISION", "EXEC", "TRIGGER", "PROFILE", "ENV", "STATUS")
	for _, r := range rows {
		fmt.Fprintf(out, "%-22s %-22s %-22s %-20s %-16s %-12s %s\n",
			dash(r.CreatedAt), dash(r.RevisionKey), dash(r.ExecutionKey),
			dash(r.TriggerName), dash(r.Profile), dash(r.Environment), dash(r.Status))
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
