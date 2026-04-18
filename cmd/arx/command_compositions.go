package main

import "github.com/spf13/cobra"

var compositionsCmd = &cobra.Command{
	Use:     "compositions [composition]",
	Aliases: []string{"composition"},
	Short:   "Manage compositions",
	Long:    "List and inspect available compositions. Use 'arx compositions' to list all, or 'arx compositions <name>' for details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listCompositions(args)
	},
}

var compositionsListCmd = &cobra.Command{
	Use:   "list [composition]",
	Short: "List available compositions",
	Long:  "List available compositions with descriptions and fields. Optionally specify a composition for detailed information.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listCompositions(args)
	},
}

func registerCompositionsCommand(root *cobra.Command) {
	root.AddCommand(compositionsCmd)
	compositionsCmd.AddCommand(compositionsListCmd)

	compositionsListCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")
	compositionsListCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details (with -l)")

	compositionsCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details")
}
