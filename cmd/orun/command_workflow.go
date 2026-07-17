package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/workflowbackend"
)

var workflowRunSet []string

// workflowCmd is the standalone authoring on-ramp for torkflow workflows
// (specs/orun-workflows WF6): validate / digest / run / view a workflow file
// directly, before dropping it into a `workflow:` plan step or blueprint hook.
// run and view front the pinned engine, sharing WF0's engine-resolution path.
var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Validate, digest, run, or view a torkflow workflow file",
	Long: `Author and debug torkflow workflows standalone, before wiring them into a
workflow: plan step or blueprint hook.

  orun workflow validate <file>   check the file parses as a torkflow workflow
  orun workflow digest   <file>   print the content digest orun would pin
  orun workflow run      <file>   run it through the pinned engine (ORUN_TORKFLOW_ENGINE)
  orun workflow view     <file>   render its DAG via the pinned engine`,
}

var workflowValidateCmd = &cobra.Command{
	Use:   "validate <file>",
	Short: "Check a workflow file parses as a torkflow workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowValidate(cmd, args[0])
	},
}

var workflowDigestCmd = &cobra.Command{
	Use:   "digest <file>",
	Short: "Print the content digest orun would pin for a workflow file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		digest, err := workflowbackend.WorkflowDigest(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), digest)
		return nil
	},
}

var workflowRunCmd = &cobra.Command{
	Use:   "run <file>",
	Short: "Run a workflow through the pinned engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowRun(cmd.Context(), cmd, args[0])
	},
}

var workflowEngineDigestCmd = &cobra.Command{
	Use:   "engine-digest",
	Short: "Print the resolved workflow engine's content digest (for intent execution.workflowEngine)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), eng.Digest())
		return nil
	},
}

var workflowViewCmd = &cobra.Command{
	Use:   "view <file>",
	Short: "Render a workflow's DAG via the pinned engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowView(cmd.Context(), cmd, args[0])
	},
}

func registerWorkflowCommand(root *cobra.Command) {
	root.AddCommand(workflowCmd)
	workflowCmd.AddCommand(workflowValidateCmd)
	workflowCmd.AddCommand(workflowDigestCmd)
	workflowCmd.AddCommand(workflowRunCmd)
	workflowCmd.AddCommand(workflowViewCmd)
	workflowCmd.AddCommand(workflowEngineDigestCmd)

	workflowRunCmd.Flags().StringArrayVar(&workflowRunSet, "set", nil, "Set a Trigger input as key=value (repeatable)")
}

// runWorkflowValidate performs a lightweight, engine-free structural check: the
// file must be readable and declare a torkflow apiVersion. A full schema check is
// the engine's job (orun stays engine-agnostic).
func runWorkflowValidate(cmd *cobra.Command, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !looksLikeWorkflow(data) {
		return fmt.Errorf("%s does not look like a torkflow workflow (no 'apiVersion: torkflow/...')", path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (%s)\n", path, workflowbackend.DigestBytes(data))
	return nil
}

// looksLikeWorkflow reports whether the bytes declare a torkflow apiVersion.
func looksLikeWorkflow(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "apiVersion:") {
			return strings.Contains(line, "torkflow/")
		}
	}
	return false
}

func runWorkflowRun(ctx context.Context, cmd *cobra.Command, path string) error {
	eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
	if err != nil {
		return err
	}
	with, err := parseSetFlags(workflowRunSet)
	if err != nil {
		return err
	}
	res, err := workflowbackend.RunStep(ctx, eng, workflowbackend.StepSpec{
		WorkflowPath: path,
		With:         with,
		Metadata:     map[string]any{"source": "orun workflow run"},
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "workflow %s: %s\n", path, res.Status)
	for _, s := range res.Steps {
		fmt.Fprintf(out, "  - %s: %s\n", s.Name, s.Status)
	}
	if !res.Succeeded() {
		msg := res.Error
		if msg == "" {
			msg = "workflow reported status " + res.Status
		}
		return fmt.Errorf("workflow failed: %s", msg)
	}
	return nil
}

// runWorkflowView fronts the pinned engine's own `view` subcommand, streaming its
// output. orun does not parse the workflow itself — it defers rendering to the
// engine it already pins (design §5/§6).
func runWorkflowView(ctx context.Context, cmd *cobra.Command, path string) error {
	eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, eng.Bin, "view", path) //nolint:gosec // pinned engine path, no shell
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = cmd.ErrOrStderr()
	return c.Run()
}

// parseSetFlags turns repeated key=value flags into a Trigger inputs map.
func parseSetFlags(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --set %q: expected key=value", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}
