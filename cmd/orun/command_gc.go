package main

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/objgc"
	"github.com/sourceplane/orun/internal/objindex"
	"github.com/sourceplane/orun/internal/state"
	"github.com/spf13/cobra"
)

var (
	gcDryRun   bool
	gcAll      bool
	gcMaxCount int
	gcMaxDays  int
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Clean up old executions and orphan plans",
	Long:  "Remove old execution records and orphan plan files based on retention policy.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGC()
	},
}

func registerGCCommand(root *cobra.Command) {
	root.AddCommand(gcCmd)

	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "Show what would be deleted")
	gcCmd.Flags().BoolVar(&gcAll, "all", false, "Delete all executions")
	gcCmd.Flags().IntVar(&gcMaxCount, "keep", 10, "Keep last N executions")
	gcCmd.Flags().IntVar(&gcMaxDays, "max-age", 30, "Remove executions older than N days")
}

func runGC() error {
	// M12: reachability GC over the object graph when active and present.
	if objStore, objRefs, objRoot, ok := openObjectStores(); ok {
		keep := gcMaxCount
		if gcAll {
			keep = 0
		}
		ix := objindex.New(objStore, objRefs, objRoot)
		res, err := objgc.Collect(context.Background(), objStore, objRefs, ix, objgc.Options{
			KeepExecutions: keep,
			DryRun:         gcDryRun,
		})
		if err != nil {
			return err
		}
		if !gcDryRun {
			_ = ix.Reindex(context.Background())
		}
		action := "Removed"
		if gcDryRun {
			action = "Would remove"
		}
		fmt.Printf("%s %d objects (%d execution refs pruned)\n", action, res.Swept, res.PrunedExecRefs)
		return nil
	}

	store := state.NewStore(storeDir())

	maxCount := gcMaxCount
	maxDays := gcMaxDays
	if gcAll {
		maxCount = 0
		maxDays = 0
	}

	removed, err := store.GC(maxCount, maxDays, gcDryRun)
	if err != nil {
		return err
	}

	if len(removed) == 0 {
		fmt.Println("Nothing to clean up.")
		return nil
	}

	action := "Removed"
	if gcDryRun {
		action = "Would remove"
	}

	for _, id := range removed {
		fmt.Printf("  %s %s\n", action, id)
	}
	fmt.Printf("\n%s %d items\n", action, len(removed))
	return nil
}
