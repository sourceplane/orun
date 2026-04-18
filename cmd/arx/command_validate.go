package main

import "github.com/spf13/cobra"

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate intent and jobs YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		return validateFiles()
	},
}

func registerValidateCommand(root *cobra.Command) {
	root.AddCommand(validateCmd)

	validateCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	validateCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
}
