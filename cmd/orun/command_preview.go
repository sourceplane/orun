package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/preview"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// command_preview.go — `orun preview`: run the repository's declared live
// preview (saas-parallel-baselining PB-12). The dev servers run detached, in
// sessions of their own, with state and one log per component under
// .orun/preview/; `orun agent serve` keeps the same preview running for a
// coding session.

var (
	previewTimeout time.Duration
	previewJSON    bool
	previewFollow  bool
)

func registerPreviewCommand(root *cobra.Command) {
	startRun := func(cmd *cobra.Command, args []string) error { return runPreviewStart(cmd) }
	previewCmd := &cobra.Command{
		Use:   "preview",
		Short: "Run the repository's declared live preview (start, status, stop, logs)",
		Long: `Run the dev server of the component intent.yaml names in preview.component,
after the previews of the components its spec.preview needs, each from its
component's directory:

  # intent.yaml
  preview: { component: web }

  # web/component.yaml
  spec:
    preview:
      run: ["pnpm", "dev", "--host", "0.0.0.0", "--port", "4173"]
      port: 4173
      needs: [api]   # previews that start first
      path: /        # the page the preview opens
      env: {}        # plain, non-secret env

The servers run detached, with the checkout's environment minus its secrets,
PORT set to their port, and spec.preview.env. start waits for each port to
answer on 127.0.0.1 and writes .orun/preview/state.json; a preview already
running is left alone. 'orun preview' alone is 'orun preview start'.`,
		Args: cobra.NoArgs,
		RunE: startRun,
	}
	addStartFlags := func(c *cobra.Command) {
		c.Flags().DurationVar(&previewTimeout, "timeout", preview.DefaultTimeout, "how long to wait for each component's port")
		c.Flags().BoolVar(&previewJSON, "json", false, "print the preview state as JSON")
	}
	addStartFlags(previewCmd)

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the preview and wait until it answers",
		Args:  cobra.NoArgs,
		RunE:  startRun,
	}
	addStartFlags(startCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the preview's state, re-checked against its processes and port",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := previewRoot(cmd)
			if err != nil {
				return err
			}
			st, err := preview.Status(root)
			if errors.Is(err, preview.ErrNoState) {
				if previewJSON {
					return printPreviewJSON(cmd.OutOrStdout(), preview.State{State: preview.StateStopped, Processes: []preview.Process{}})
				}
				fmt.Fprintln(cmd.OutOrStdout(), "no preview has been started here")
				return nil
			}
			if err != nil {
				return err
			}
			if previewJSON {
				return printPreviewJSON(cmd.OutOrStdout(), st)
			}
			printPreviewState(cmd.OutOrStdout(), root, st)
			return nil
		},
	}
	statusCmd.Flags().BoolVar(&previewJSON, "json", false, "print the preview state as JSON")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop every process of the preview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := previewRoot(cmd)
			if err != nil {
				return err
			}
			st, err := preview.Stop(root)
			if errors.Is(err, preview.ErrNoState) {
				fmt.Fprintln(cmd.OutOrStdout(), "no preview has been started here")
				return nil
			}
			if err != nil {
				return err
			}
			if previewJSON {
				return printPreviewJSON(cmd.OutOrStdout(), st)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "preview stopped (%d process(es))\n", len(st.Processes))
			return nil
		},
	}
	stopCmd.Flags().BoolVar(&previewJSON, "json", false, "print the preview state as JSON")

	logsCmd := &cobra.Command{
		Use:   "logs [component]",
		Short: "Print a component's preview log (the shown component by default)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := previewRoot(cmd)
			if err != nil {
				return err
			}
			component := ""
			if len(args) == 1 {
				component = args[0]
			} else if st, err := preview.ReadState(root); err == nil && st.Component != "" {
				component = st.Component
			} else if p, err := preview.Load(previewIntentPath(cmd, root)); err == nil {
				component = p.Target().Name
			} else {
				return fmt.Errorf("name a component: no preview has been started here")
			}
			return preview.CopyLog(cmd.Context(), cmd.OutOrStdout(), root, component, previewFollow)
		},
	}
	logsCmd.Flags().BoolVarP(&previewFollow, "follow", "f", false, "keep printing what the server logs")

	previewCmd.AddCommand(startCmd, statusCmd, stopCmd, logsCmd)
	root.AddCommand(previewCmd)
}

func runPreviewStart(cmd *cobra.Command) error {
	root, err := previewRoot(cmd)
	if err != nil {
		return err
	}
	plan, err := preview.Load(previewIntentPath(cmd, root))
	if err != nil {
		return fmt.Errorf("preview: %w", err)
	}
	st, err := preview.Start(cmd.Context(), plan, preview.Options{Timeout: previewTimeout})
	out := cmd.OutOrStdout()
	if previewJSON && st.State != "" {
		if pErr := printPreviewJSON(out, st); pErr != nil {
			return pErr
		}
	} else if err == nil {
		printPreviewState(out, root, st)
	}
	if err != nil && st.State == preview.StateFailed {
		return fmt.Errorf("preview failed: %w (logs: orun preview logs %s)", err, st.Component)
	}
	if err != nil {
		return fmt.Errorf("preview: %w", err)
	}
	return nil
}

// previewRoot is the repository the preview belongs to: the intent's
// directory (--intent, else the nearest intent.yaml), else the working
// directory.
func previewRoot(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("intent") {
		abs, err := filepath.Abs(intentFile)
		if err != nil {
			return "", err
		}
		return filepath.Dir(abs), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, dir, err := discovery.FindIntentFile(cwd); err == nil {
		return dir, nil
	}
	return cwd, nil
}

// previewIntentPath is the intent file under root that previewRoot found.
func previewIntentPath(cmd *cobra.Command, root string) string {
	if cmd.Flags().Changed("intent") {
		return intentFile
	}
	if p := preview.FindIntent(root); p != "" {
		return p
	}
	return filepath.Join(root, "intent.yaml")
}

// formatPreviewErrors is `orun validate`'s report of bad preview
// declarations.
func formatPreviewErrors(errs []error) error {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("preview validation failed:\n  %s", strings.Join(msgs, "\n  "))
}

func printPreviewJSON(w io.Writer, st preview.State) error {
	if st.Processes == nil {
		st.Processes = []preview.Process{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

func printPreviewState(w io.Writer, root string, st preview.State) {
	color := ui.ColorEnabledForWriter(w)
	mark := ui.Yellow(color, "…")
	switch st.State {
	case preview.StateReady:
		mark = ui.Green(color, "✓")
	case preview.StateFailed:
		mark = ui.Red(color, "✕")
	case preview.StateStopped:
		mark = ui.Dim(color, "■")
	}
	fmt.Fprintf(w, "%s preview %s — %s on http://127.0.0.1:%d%s\n", mark, st.State, st.Component, st.Port, st.Path)
	if st.Detail != "" {
		fmt.Fprintf(w, "  %s\n", st.Detail)
	}
	for _, p := range st.Processes {
		fmt.Fprintf(w, "  %s pid %-7d :%d\n", ui.Dim(color, fmt.Sprintf("%-16s", p.Component)), p.PID, p.Port)
	}
	if rel, err := filepath.Rel(mustCwd(), preview.StatePath(root)); err == nil {
		fmt.Fprintf(w, "  %s %s\n", ui.Dim(color, "state:"), rel)
	}
	fmt.Fprintf(w, "  %s orun preview logs %s\n", ui.Dim(color, "logs: "), st.Component)
}

func mustCwd() string {
	cwd, _ := os.Getwd()
	return cwd
}
