package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/artifactstore/github"
	"github.com/sourceplane/orun/internal/runbundle"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	githubRunsWorkflow string
	githubRunsBranch   string
	githubRunsSHA      string
	githubRunsFailed   bool
	githubRunsLimit    int
	githubRunsDetails  bool

	githubPullRunID      int64
	githubPullExecID     string
	githubPullSHA        string
	githubPullBranch     string
	githubPullLatest     bool
	githubPullFailed     bool
	githubPullIncludeRaw bool
	githubPullOrunDir    string

	githubLogsRunID  int64
	githubLogsExecID string
	githubLogsSHA    string
	githubLogsBranch string
	githubLogsFailed bool
	githubLogsLatest bool
	githubLogsJob    string
)

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "Inspect GitHub Actions artifacts and workflow runs",
	Long:  "Inspect, pull, and manage GitHub Actions artifact shards.\n\nRequires GITHUB_TOKEN, GH_TOKEN, or gh auth token to authenticate.",
}

var githubRunsCmd = &cobra.Command{
	Use:   "runs",
	Short: "List GitHub Actions workflow runs",
	Long:  "List workflow runs and inspect their artifact shards.\n\nThree levels of detail:\n  Level 1 (default): List workflow runs + parse exec-id, role, status from artifact names.\n  Level 2 (--details): Download plan shard manifests for exact status.\n  Level 3 (orun github pull): Full shard download + hydrate.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGithubRuns()
	},
}

var githubPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download and hydrate artifact shards from a workflow run",
	Long:  "Download all Orun artifact shards from a GitHub Actions workflow run, synthesize the execution, and hydrate it into the local .orun/executions/ layout.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGithubPull()
	},
}

var githubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Quick remote status of a workflow run",
	Long:  "Download only manifest files (no logs) from GitHub Actions artifact shards for a quick remote status summary.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGithubStatus()
	},
}

var githubLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Download logs from a workflow run's artifact shards",
	Long:  "Download specific job artifact shard logs from a GitHub Actions workflow run.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGithubLogs()
	},
}

func registerGithubCommand(root *cobra.Command) {
	root.AddCommand(githubCmd)

	// Runs subcommand
	githubCmd.AddCommand(githubRunsCmd)
	githubRunsCmd.Flags().StringVar(&githubRunsWorkflow, "workflow", "orun.yaml", "Workflow filename filter")
	githubRunsCmd.Flags().StringVar(&githubRunsBranch, "branch", "", "Branch filter")
	githubRunsCmd.Flags().StringVar(&githubRunsSHA, "sha", "", "Commit SHA filter")
	githubRunsCmd.Flags().BoolVar(&githubRunsFailed, "failed", false, "Show only failed runs")
	githubRunsCmd.Flags().IntVar(&githubRunsLimit, "limit", 10, "Max runs to show")
	githubRunsCmd.Flags().BoolVar(&githubRunsDetails, "details", false, "Download manifests for accurate status")

	// Pull subcommand
	githubCmd.AddCommand(githubPullCmd)
	githubPullCmd.Flags().Int64Var(&githubPullRunID, "run-id", 0, "Explicit GitHub run ID")
	githubPullCmd.Flags().StringVar(&githubPullExecID, "exec-id", "", "Execution ID (gh-<run>-<attempt>-<sha>)")
	githubPullCmd.Flags().StringVar(&githubPullSHA, "sha", "", "Pull latest for this SHA")
	githubPullCmd.Flags().StringVar(&githubPullBranch, "branch", "", "Pull latest for this branch")
	githubPullCmd.Flags().BoolVar(&githubPullLatest, "latest", false, "Pull latest run")
	githubPullCmd.Flags().BoolVar(&githubPullFailed, "failed", false, "Pull latest failed run")
	githubPullCmd.Flags().BoolVar(&githubPullIncludeRaw, "include-raw", false, "Include unredacted logs")
	githubPullCmd.Flags().StringVar(&githubPullOrunDir, "orun-dir", ".", "Target .orun directory")

	// Status subcommand
	githubCmd.AddCommand(githubStatusCmd)

	// Logs subcommand
	githubCmd.AddCommand(githubLogsCmd)
	githubLogsCmd.Flags().Int64Var(&githubLogsRunID, "run-id", 0, "Explicit GitHub run ID")
	githubLogsCmd.Flags().StringVar(&githubLogsExecID, "exec-id", "", "Execution ID (gh-<run>-<attempt>-<sha>)")
	githubLogsCmd.Flags().StringVar(&githubLogsSHA, "sha", "", "Latest run for this SHA")
	githubLogsCmd.Flags().StringVar(&githubLogsBranch, "branch", "", "Latest run for this branch")
	githubLogsCmd.Flags().BoolVar(&githubLogsFailed, "failed", false, "Latest failed run")
	githubLogsCmd.Flags().BoolVar(&githubLogsLatest, "latest", false, "Latest run")
	githubLogsCmd.Flags().StringVar(&githubLogsJob, "job", "", "Job ID to fetch logs for")
}

// resolveRepoFromEnv returns the GitHub repository from environment or git remote.
func resolveRepoFromEnv() string {
	if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" {
		return repo
	}
	// Fall back to git remote parsing
	return detectRepoFromGitRemote()
}

// detectRepoFromGitRemote tries to parse owner/repo from git remote origin.
func detectRepoFromGitRemote() string {
	// Best-effort: read from git remote
	data, err := execCommand("git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(data))
	return parseGitHubRepo(url)
}

// parseGitHubRepo extracts owner/repo from various GitHub remote URL formats.
func parseGitHubRepo(url string) string {
	// git@github.com:owner/repo.git
	// https://github.com/owner/repo.git
	for _, prefix := range []string{"git@github.com:", "https://github.com/", "https://api.github.com/repos/"} {
		if strings.Contains(url, prefix) {
			parts := strings.SplitN(url, prefix, 2)
			if len(parts) == 2 {
				repo := strings.TrimSuffix(parts[1], ".git")
				repo = strings.TrimSuffix(repo, "/")
				if strings.Contains(repo, "/") {
					return repo
				}
			}
		}
	}
	return ""
}

// execCommand runs a command and returns stdout.
var execCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}

func runGithubRuns() error {
	repo := resolveRepoFromEnv()
	if repo == "" {
		return fmt.Errorf("unable to determine GitHub repository; set GITHUB_REPOSITORY or ensure a git remote is configured")
	}

	ctx := context.Background()
	client, err := github.NewClient(ctx, repo)
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}

	opts := github.ListRunOptions{
		Branch:  githubRunsBranch,
		SHA:     githubRunsSHA,
		PerPage: githubRunsLimit,
	}
	if githubRunsFailed {
		opts.Status = "failure"
	}

	runs, err := client.ListWorkflowRuns(ctx, opts)
	if err != nil {
		return fmt.Errorf("list workflow runs: %w", err)
	}

	if len(runs) == 0 {
		fmt.Println("No workflow runs found.")
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	for _, run := range runs {
		// List artifacts for each run
		artifacts, err := client.ListArtifacts(ctx, run.ID)
		if err != nil {
			// Best-effort; show run without artifacts
			artifacts = nil
		}

		orunArtifacts := filterOrunShards(artifacts)
		status := run.Conclusion
		if status == "" {
			status = run.Status
		}

		summary := fmt.Sprintf("%d  %s  %s  %s  %s",
			run.ID,
			fmt.Sprintf("%.7s", run.HeadSHA),
			run.HeadBranch,
			status,
			formatTimeAgo(run.CreatedAt),
		)

		if len(orunArtifacts) > 0 {
			summary += fmt.Sprintf("  [%d shards]", len(orunArtifacts))
		} else {
			summary += "  [no orun artifacts]"
		}

		if color {
			fmt.Printf("  %s\n", ui.Dim(color, summary))
		} else {
			fmt.Println(summary)
		}
	}

	return nil
}

func runGithubPull() error {
	repo := resolveRepoFromEnv()
	if repo == "" {
		return fmt.Errorf("unable to determine GitHub repository; set GITHUB_REPOSITORY or ensure a git remote is configured")
	}

	ctx := context.Background()
	client, err := github.NewClient(ctx, repo)
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}

	// Resolve the workflow run
	resolveOpts := github.ResolveOpts{
		RunID:  githubPullRunID,
		ExecID: githubPullExecID,
		SHA:    githubPullSHA,
		Branch: githubPullBranch,
		Failed: githubPullFailed,
	}
	if githubPullLatest && resolveOpts.RunID == 0 && resolveOpts.ExecID == "" && resolveOpts.SHA == "" {
		resolveOpts = github.ResolveOpts{Branch: githubPullBranch}
	}

	run, err := github.ResolveRun(ctx, client, resolveOpts)
	if err != nil {
		return fmt.Errorf("resolve workflow run: %w", err)
	}

	// List Orun artifacts for this run
	shards, err := client.ListOrunArtifacts(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	if len(shards) == 0 {
		fmt.Fprintf(os.Stderr, "No Orun artifacts found for run %d\n", run.ID)
		return nil
	}

	// Group by execID
	execGroups := groupByExecID(shards)
	if len(execGroups) == 0 {
		fmt.Fprintf(os.Stderr, "No valid Orun artifacts found for run %d\n", run.ID)
		return nil
	}

	// Use the first (or only) execution group
	var targetExecID string
	var targetShards []artifactstore.RemoteShard
	for eid, s := range execGroups {
		targetExecID = eid
		targetShards = s
		break // first group
	}

	orunDir := githubPullOrunDir
	if orunDir == "." {
		orunDir = filepath.Join(storeDir(), state.OrunDir)
	}

	// Download all shards
	fmt.Fprintf(os.Stderr, "Downloading %d shard(s) for %s...\n", len(targetShards), targetExecID)

	destDir, err := os.MkdirTemp("", "orun-pull-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(destDir)

	var planShard *artifactstore.DownloadedShard
	var jobShards []*runbundle.JobShard

	for _, s := range targetShards {
		shardDest := filepathJoin(destDir, s.ID)
		ds, err := client.Download(ctx, s, shardDest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ warning: failed to download shard %s: %v\n", s.Name, err)
			continue
		}
		if ds.Shard != nil && ds.Shard.Role == runbundle.ShardRolePlan {
			planShard = ds
		} else if ds.Shard != nil && ds.Shard.Role == runbundle.ShardRoleJob {
			// Convert DownloadedShard to JobShard
			jobShards = append(jobShards, &runbundle.JobShard{
				Manifest: ds.Shard,
				Dir:      ds.Dir,
			})
		}
	}

	if planShard == nil {
		return fmt.Errorf("no plan shard found in run %d", run.ID)
	}

	// Synthesize execution
	planShardData := &runbundle.PlanShard{
		Manifest: planShard.Shard,
		Dir:      planShard.Dir,
	}

	synthesized, err := runbundle.Synthesize(planShardData, jobShards)
	if err != nil {
		return fmt.Errorf("synthesize execution: %w", err)
	}

	// Hydrate to local .orun directory
	_, err = runbundle.Hydrate(ctx, planShardData, jobShards, runbundle.HydrateOptions{
		ExecID:     targetExecID,
		Source:     planShard.Shard.Source,
		Overwrite:  false,
		IncludeRaw: githubPullIncludeRaw,
	}, orunDir)
	if err != nil {
		return fmt.Errorf("hydrate execution: %w", err)
	}

	summary := runbundle.SynthesizedSummary(synthesized)
	fmt.Fprintf(os.Stderr, "✓ hydrated %s  %s\n", targetExecID, summary)

	return nil
}

func runGithubStatus() error {
	// Lightweight: list artifacts and show shard-level status
	repo := resolveRepoFromEnv()
	if repo == "" {
		return fmt.Errorf("unable to determine GitHub repository; set GITHUB_REPOSITORY or ensure a git remote is configured")
	}

	ctx := context.Background()
	client, err := github.NewClient(ctx, repo)
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}

	// For status, default to the latest run
	run, err := github.ResolveRun(ctx, client, github.ResolveOpts{
		RunID:  githubLogsRunID,
		ExecID: githubLogsExecID,
		SHA:    githubLogsSHA,
		Branch: githubLogsBranch,
		Failed: githubLogsFailed,
	})
	if err != nil {
		return fmt.Errorf("resolve workflow run: %w", err)
	}

	shards, err := client.ListOrunArtifacts(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Run %d (%s): %d Orun artifact(s)\n", run.ID, run.HeadBranch, len(shards))

	// Group by execID and show per-execution summary
	execGroups := groupByExecID(shards)
	for execID, shards := range execGroups {
		var plans, jobs int
		for _, s := range shards {
			if s.Parsed != nil && s.Parsed.Role == runbundle.ShardRolePlan {
				plans++
			} else {
				jobs++
			}
		}
		fmt.Printf("  %s  %d plan(s), %d job(s)\n", execID, plans, jobs)
	}

	return nil
}

func runGithubLogs() error {
	repo := resolveRepoFromEnv()
	if repo == "" {
		return fmt.Errorf("unable to determine GitHub repository; set GITHUB_REPOSITORY or ensure a git remote is configured")
	}

	ctx := context.Background()
	client, err := github.NewClient(ctx, repo)
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}

	// Resolve the workflow run
	run, err := github.ResolveRun(ctx, client, github.ResolveOpts{
		RunID:  githubLogsRunID,
		ExecID: githubLogsExecID,
		SHA:    githubLogsSHA,
		Branch: githubLogsBranch,
		Failed: githubLogsFailed,
	})
	if err != nil {
		return fmt.Errorf("resolve workflow run: %w", err)
	}

	shards, err := client.ListOrunArtifacts(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	if len(shards) == 0 {
		fmt.Fprintf(os.Stderr, "No Orun artifacts found for run %d\n", run.ID)
		return nil
	}

	// If a specific job was requested, filter by job name
	targetShards := shards
	if githubLogsJob != "" {
		var filtered []artifactstore.RemoteShard
		for _, s := range shards {
			if s.Parsed != nil && strings.Contains(s.Name, githubLogsJob) {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no artifacts matching job %q in run %d", githubLogsJob, run.ID)
		}
		targetShards = filtered
	}

	// Download to temp and print log files
	destDir, err := os.MkdirTemp("", "orun-logs-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(destDir)

	for _, s := range targetShards {
		shardDest := filepathJoin(destDir, s.ID)
		ds, err := client.Download(ctx, s, shardDest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ warning: failed to download %s: %v\n", s.Name, err)
			continue
		}
		fmt.Printf("=== %s ===\n", s.Name)
		if ds.Shard != nil {
			for logicalName := range ds.Shard.Files {
				fmt.Printf("  %s\n", logicalName)
			}
		}
	}

	return nil
}

// filterOrunShards returns only artifacts matching the Orun naming scheme.
func filterOrunShards(shards []artifactstore.RemoteShard) []artifactstore.RemoteShard {
	var result []artifactstore.RemoteShard
	for _, s := range shards {
		if s.Parsed != nil {
			result = append(result, s)
		}
	}
	return result
}

// groupByExecID groups remote shards by their execution ID.
func groupByExecID(shards []artifactstore.RemoteShard) map[string][]artifactstore.RemoteShard {
	groups := make(map[string][]artifactstore.RemoteShard)
	for _, s := range shards {
		if s.Parsed != nil {
			groups[s.Parsed.ExecID] = append(groups[s.Parsed.ExecID], s)
		}
	}
	return groups
}

// formatTimeAgo returns a human-readable time difference.
func formatTimeAgo(rfc3339 string) string {
	return rfc3339
}

// filepathJoin joins path elements. Separate from filepath.Join for testing.
var filepathJoin = func(elem ...string) string {
	if len(elem) == 0 {
		return ""
	}
	result := elem[0]
	for _, e := range elem[1:] {
		result += "/" + e
	}
	return result
}

// Ensure compile-time check for unused imports
var _ = artifactstore.RemoteShard{}
var _ = runbundle.HydrateOptions{}