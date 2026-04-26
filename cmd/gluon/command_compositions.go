package main

import "github.com/spf13/cobra"

var compositionsCmd = &cobra.Command{
	Use:     "compositions [composition]",
	Aliases: []string{"composition"},
	Short:   "Manage compositions",
	Long:    "List and inspect available compositions. Use 'gluon compositions' to list all, or 'gluon compositions <name>' for details.",
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

var compositionsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Resolve and cache declared composition sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		return pullCompositions()
	},
}

var compositionsLockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Resolve declared composition sources and write a lock file",
	RunE: func(cmd *cobra.Command, args []string) error {
		return lockCompositions()
	},
}

var compositionsPackageCmd = &cobra.Command{
	Use:   "package",
	Short: "Build and publish composition packages",
}

var compositionsPackageBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a composition package archive from a package root",
	RunE: func(cmd *cobra.Command, args []string) error {
		return buildCompositionPackage()
	},
}

var compositionsPackagePushCmd = &cobra.Command{
	Use:   "push <archive> <oci-ref>",
	Short: "Push a composition package archive to an OCI registry",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return pushCompositionPackage(args[0], args[1])
	},
}

func registerCompositionsCommand(root *cobra.Command) {
	root.AddCommand(compositionsCmd)
	compositionsCmd.AddCommand(compositionsListCmd)
	compositionsCmd.AddCommand(compositionsPullCmd)
	compositionsCmd.AddCommand(compositionsLockCmd)
	compositionsCmd.AddCommand(compositionsPackageCmd)
	compositionsPackageCmd.AddCommand(compositionsPackageBuildCmd)
	compositionsPackageCmd.AddCommand(compositionsPackagePushCmd)

	compositionsListCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")
	compositionsListCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details (with -l)")
	compositionsPackageBuildCmd.Flags().StringVar(&compositionPackageRoot, "root", "", "Composition package root directory")
	compositionsPackageBuildCmd.Flags().StringVarP(&compositionPackageOutput, "output", "o", "", "Output .tgz archive path")

	compositionsCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details")
}
