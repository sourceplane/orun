package main

import "github.com/spf13/cobra"

var (
	planName       string
	planComponents []string
)

var planCmd = &cobra.Command{
	Use:   "plan [component]",
	Short: "Generate execution plan from intent",
	Long:  "Generate an execution plan from intent.yaml.\n\nOptionally pass a component name to scope the plan to that component only.\nEquivalent to --component <name> but more convenient for quick runs.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			planComponents = append(planComponents, args[0])
		}
		return generatePlan()
	},
}

func registerPlanCommand(root *cobra.Command) {
	root.AddCommand(planCmd)

	planCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output plan file path (default: .orun/plans/)")
	planCmd.Flags().StringVarP(&outputFormat, "format", "f", "json", "Output format (json/yaml)")
	planCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
	planCmd.Flags().StringVarP(&environment, "env", "e", "", "Filter by environment")
	planCmd.Flags().StringArrayVar(&planComponents, "component", nil, "Filter by component (repeatable)")
	planCmd.Flags().StringVar(&planName, "name", "", "Named plan stored in .orun/plans/<name>.json")
	planCmd.Flags().StringVarP(&viewPlan, "view", "v", "", "View plan (dag/dependencies/component=NAME)")
	planCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	planCmd.Flags().StringVar(&baseBranch, "base", "", "Base ref for changed detection (default: main)")
	planCmd.Flags().StringVar(&headRef, "head", "", "Head ref for changed detection (usually HEAD)")
	planCmd.Flags().StringSliceVar(&changedFiles, "files", nil, "Comma-separated changed files (overrides git diff calculation)")
	planCmd.Flags().BoolVar(&uncommitted, "uncommitted", false, "Use only uncommitted changes")
	planCmd.Flags().BoolVar(&untracked, "untracked", false, "Use only untracked files")
	planCmd.Flags().BoolVar(&explainChanged, "explain", false, "Show how --changed refs were resolved")
}
