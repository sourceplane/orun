package main

import "github.com/spf13/cobra"

var componentCmd = &cobra.Command{
	Use:     "component [component-name]",
	Aliases: []string{"components"},
	Short:   "List and analyze components",
	Long:    "List all components with their merged properties. Use 'gluon component <name>' for details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listComponents(args)
	},
}

func registerComponentCommand(root *cobra.Command) {
	root.AddCommand(componentCmd)

	componentCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	componentCmd.Flags().StringVar(&baseBranch, "base", "", "Base ref for changed detection (default: main)")
	componentCmd.Flags().StringVar(&headRef, "head", "", "Head ref for changed detection (usually HEAD)")
	componentCmd.Flags().StringSliceVar(&changedFiles, "files", nil, "Comma-separated changed files (overrides git diff calculation)")
	componentCmd.Flags().BoolVar(&uncommitted, "uncommitted", false, "Use only uncommitted changes")
	componentCmd.Flags().BoolVar(&untracked, "untracked", false, "Use only untracked files")
	componentCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")
}
