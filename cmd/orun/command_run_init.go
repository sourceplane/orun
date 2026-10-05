package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
)

// `orun run init` creates (or joins) the remote run for a plan and exits
// without claiming a job. A CI plan job runs it before the matrix fans out, so
// the run's coordinator exists before the first lane is queued on GitHub: the
// stage-2 gate parks a queued lane on its run, and a lane whose run does not
// exist yet would fall through to the fleet ungated (orun-managed-runners
// design §2, §4.2). Every lane then runs `orun run --job … --exec-id <same>`
// and joins the run InitRun already created, exactly as today when the first
// lane creates it.
var runInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create the remote run for a plan before any lane claims a job",
	Long: `Create (or idempotently join) the remote run for a plan without executing or
claiming any job, so the run exists on the platform before the first CI lane
is queued. Lanes then run 'orun run --job <id> --exec-id <same id>' and join it.

Requires remote state: --remote-state, ORUN_REMOTE_STATE=true, or
execution.state in intent.yaml.`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit()
	},
}

func registerRunInitCommand(parent *cobra.Command) {
	parent.AddCommand(runInitCmd)
	runInitCmd.Flags().StringVarP(&runPlanRef, "plan", "p", "", "Plan reference: file path, name, or checksum prefix")
	runInitCmd.Flags().StringVar(&runExecID, "exec-id", "", "Execution ID the lanes will pass to 'orun run --job' (or set ORUN_EXEC_ID)")
	runInitCmd.Flags().StringVarP(&runEnv, "env", "e", "", "Environment the run is for (single environment, optional)")
	runInitCmd.Flags().BoolVar(&runRemoteState, "remote-state", false, "Use the Orun Cloud backend (sets ORUN_REMOTE_STATE=true)")
	runInitCmd.Flags().StringVar(&runBackendURL, "backend-url", "", "Backend URL for remote state (or set ORUN_BACKEND_URL)")
	runInitCmd.Flags().StringVar(&runOrg, "workspace", "", "Workspace scope for remote state (overrides the linked workspace; or set ORUN_WORKSPACE)")
	runInitCmd.Flags().StringVar(&runOrg, "org", "", "Alias of --workspace (legacy spelling; or set ORUN_ORG)")
	runInitCmd.Flags().StringVar(&runProject, "project", "", "Advanced: override the project scope (or set ORUN_PROJECT)")
	runInitCmd.Flags().StringVarP(&runWorkDir, "workdir", "w", ".", "Working directory (git provenance is read from here)")
}

func runInit() error {
	plan, err := resolveAndLoadPlan()
	if err != nil {
		return err
	}

	var loadedIntent *model.Intent
	if intentFile != "" {
		if si, _, loadErr := loadResolvedIntentFile(intentFile); loadErr == nil {
			loadedIntent = si
		}
	}
	if !isRemoteStateActive(loadedIntent) {
		return fmt.Errorf("orun run init needs remote state: pass --remote-state, set ORUN_REMOTE_STATE=true, or declare execution.state in intent.yaml")
	}
	backendURL := resolveBackendURL(loadedIntent)
	if backendURL == "" {
		return fmt.Errorf("--remote-state requires --backend-url or ORUN_BACKEND_URL (or intent.yaml execution.state.backendUrl)")
	}

	execID := runExecID
	if execID == "" {
		execID = os.Getenv(execIDEnvVar)
	}
	execID = remotestate.DeriveRunID(execID)

	sess, err := initRemoteRun(context.Background(), plan, execID, backendURL, loadedIntent, false)
	if err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s run %s ready: %d job(s), exec-id %s\n",
		ui.Green(color, "✓"), sess.handle.RunID, len(plan.Jobs), execID)
	return nil
}
