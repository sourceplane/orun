package main

import (
	"fmt"

	"github.com/sourceplane/gluon/internal/state"
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
