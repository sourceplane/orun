package main

import "github.com/spf13/cobra"

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug intent processing",
	RunE: func(cmd *cobra.Command, args []string) error {
		return debugIntent()
	},
}

func registerDebugCommand(root *cobra.Command) {
	root.AddCommand(debugCmd)
}
