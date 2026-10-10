package main

import "github.com/spf13/cobra"

var compositionsCmd = &cobra.Command{
	Use:     "compositions [composition]",
	Aliases: []string{"composition"},
	Short:   "Manage compositions",
	Long:    "List and inspect available compositions. Use 'orun compositions' to list all, or 'orun compositions <name>' for details.",
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
	Long: `Resolve declared composition sources and record their digests in .orun/compositions.lock.yaml.

With --write-intent, also write each resolved digest into the matching source's
digest: field in intent.yaml (comments and formatting are kept). Resolution then
enforces the pin: orun plan fails with "digest mismatch" if a source changes.

With --check, write nothing and exit non-zero when a source without digest:
resolves differently from the recorded lock.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return lockCompositions(compositionsLockWriteIntent, compositionsLockCheck)
	},
}

var (
	compositionsLockWriteIntent bool
	compositionsLockCheck       bool
)

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
	compositionsLockCmd.Flags().BoolVar(&compositionsLockWriteIntent, "write-intent", false, "Write resolved digests into the digest: field of each source in intent.yaml")
	compositionsLockCmd.Flags().BoolVar(&compositionsLockCheck, "check", false, "Exit non-zero if an unpinned source resolves differently from the lock; writes nothing")
	compositionsLockCmd.MarkFlagsMutuallyExclusive("write-intent", "check")
	compositionsPackageBuildCmd.Flags().StringVar(&compositionPackageRoot, "root", "", "Composition package root directory")
	compositionsPackageBuildCmd.Flags().StringVarP(&compositionPackageOutput, "output", "o", "", "Output .tgz archive path")

	compositionsCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details")
}
