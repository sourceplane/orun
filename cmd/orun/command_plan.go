package main

import (
	"context"

	"github.com/spf13/cobra"
)

var (
	planName             string
	planComponents       []string
	planLong             bool
	artifactBackend      string
	githubOutput         bool
	planNoCatalogRefresh bool
	planCatalogStrict    bool
	planPushCatalog      bool
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
		if err := generatePlan(); err != nil {
			return err
		}
		// --push-catalog: plan already refreshed the object-model catalog and
		// repointed catalogs/current, so the snapshot is ready to publish. Reuse
		// the `catalog push` flow to sync it and advance the head. Explicit opt-in
		// → fail loud (mirrors `catalog refresh --push`); the config-driven
		// best-effort auto-sync is a separate, quieter path.
		if planPushCatalog {
			return pushCatalogAfterPlan(cmd.Context())
		}
		// Config-driven best-effort auto-sync (execution.state.autopushCatalog).
		// Never fails the plan; a no-op unless enabled + on the clean default
		// branch + the catalog changed since the last publish.
		maybeAutoPushCatalog(cmd.Context())
		return nil
	},
}

// pushCatalogAfterPlan publishes the just-resolved catalog to the configured
// backend after a successful `plan --push-catalog`. The backend is resolved from
// --backend-url/ORUN_BACKEND_URL/intent.yaml; the head is the project-wide head
// (a dirty worktree still yields a local-only snapshot, which pushResolvedCatalog
// surfaces).
func pushCatalogAfterPlan(ctx context.Context) error {
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), "")
	if err != nil {
		return err
	}
	return pushResolvedCatalog(ctx, backendURL, "", "", "")
}

func registerPlanCommand(root *cobra.Command) {
	root.AddCommand(planCmd)

	planCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output plan file path (default: .orun/plans/)")
	planCmd.Flags().StringVarP(&outputFormat, "format", "f", "json", "Output format (json/yaml)")
	planCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
	planCmd.Flags().StringVarP(&environment, "env", "e", "", "Filter by environment (comma-separated)")
	planCmd.Flags().BoolVar(&allEnvs, "all-envs", false, "Plan all environments explicitly (mutually exclusive with --env)")
	planCmd.Flags().StringArrayVar(&planComponents, "component", nil, "Filter by component (repeatable)")
	planCmd.Flags().StringVar(&planName, "name", "", "Named plan stored in .orun/plans/<name>.json")
	planCmd.Flags().StringVarP(&viewPlan, "view", "v", "", "View plan (dag/dag:long/dependencies/component=NAME)")
	planCmd.Flags().BoolVar(&planLong, "long", false, "Show detailed output (step commands, IDs)")
	planCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	planCmd.Flags().StringVar(&baseBranch, "base", "", "Base ref for changed detection (default: main)")
	planCmd.Flags().StringVar(&headRef, "head", "", "Head ref for changed detection (usually HEAD)")
	planCmd.Flags().StringSliceVar(&changedFiles, "files", nil, "Comma-separated changed files (overrides git diff calculation)")
	planCmd.Flags().BoolVar(&uncommitted, "uncommitted", false, "Use only uncommitted changes")
	planCmd.Flags().BoolVar(&untracked, "untracked", false, "Use only untracked files")
	planCmd.Flags().BoolVar(&explainChanged, "explain", false, "Show how --changed refs were resolved")
	planCmd.Flags().StringVar(&intentImpact, "intent-impact", "watch", "How global intent changes affect components (all/watch/none)")
	planCmd.Flags().StringVar(&triggerName, "trigger", "", "Named trigger binding for environment activation")
	planCmd.Flags().StringVar(&fromCI, "from-ci", "", "CI provider for event normalization (e.g. github)")
	planCmd.Flags().StringVar(&eventFile, "event-file", "", "Path to provider event JSON file")
	planCmd.Flags().StringVar(&artifactBackend, "artifact", "", "Artifact backend for upload (e.g. github)")
	planCmd.Flags().BoolVar(&githubOutput, "github-output", false, "Write matrix/plan_id/exec_id to $GITHUB_OUTPUT")

	planCmd.Flags().BoolVar(&planNoCatalogRefresh, "no-catalog-refresh", false, "Skip catalog refresh; plan proceeds without catalog context")
	planCmd.Flags().BoolVar(&planPushCatalog, "push-catalog", false, "After planning, sync the resolved catalog snapshot to the configured backend and advance the head")
	planCmd.Flags().BoolVar(&planCatalogStrict, "catalog-strict", false, "Fail plan on catalog resolution errors")

	// env-scoping (Z model): --env and --all-envs are mutually exclusive.
	planCmd.MarkFlagsMutuallyExclusive("env", "all-envs")
	// --push-catalog publishes the catalog this run resolved, so it cannot be
	// combined with skipping the refresh (there would be nothing fresh to push).
	planCmd.MarkFlagsMutuallyExclusive("push-catalog", "no-catalog-refresh")
}
