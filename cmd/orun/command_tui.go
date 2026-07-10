package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/tui"
	"github.com/sourceplane/orun/internal/tui/services"
)

var (
	tuiRemoteState bool
	tuiBackendURL  string
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the Orun Cockpit TUI",
	Long: "Launch the interactive Orun Cockpit: browse components, generate plans, " +
		"run, and inspect logs. The TUI is a component-native control plane over " +
		"Orun internal packages — it never shells out to the orun binary.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(cmd.Context())
	},
}

func registerTuiCommand(root *cobra.Command) {
	root.AddCommand(tuiCmd)
	tuiCmd.Flags().BoolVar(&tuiRemoteState, "remote-state", false,
		"Connect to orun-backend for remote run state")
	tuiCmd.Flags().StringVar(&tuiBackendURL, "backend-url", "",
		fmt.Sprintf("orun-backend URL (or set %s)", backendURLEnvVar))
}

// resolveTUIBackend returns the remote state backend when --remote-state is
// set, else nil — the local TUI reads the content-addressed object graph
// directly (no backend). It fails closed when --remote-state is set without a
// resolvable backend URL so no half-launched TUI ever runs with invalid remote
// config.
func resolveTUIBackend() (statebackend.Backend, func(), error) {
	if !tuiRemoteState {
		return nil, func() {}, nil
	}

	backendURL := tuiBackendURL
	if backendURL == "" {
		backendURL = os.Getenv(backendURLEnvVar)
	}
	if backendURL == "" {
		return nil, nil, fmt.Errorf("--remote-state requires --backend-url or %s", backendURLEnvVar)
	}

	b, err := newRemoteBackend(backendURL)
	if err != nil {
		return nil, nil, fmt.Errorf("remote state: %w", err)
	}
	cleanup := func() { _ = b.Close(context.Background()) }
	return b, cleanup, nil
}

// buildTUIService discovers the intent root and constructs the live service
// the cockpit reads — shared by `orun tui` and `orun agent` (bare).
func buildTUIService() (services.OrunService, error) {
	if intentRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			if foundPath, foundDir, derr := discovery.FindIntentFile(cwd); derr == nil {
				intentFile = foundPath
				intentRoot = foundDir
			}
		}
	}
	backend, _, err := resolveTUIBackend()
	if err != nil {
		return nil, err
	}
	orunRoot, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, fmt.Errorf("resolve object-model root: %w", err)
	}
	return services.NewLiveOrunService(services.LiveServiceConfig{
		IntentFile:      intentFile,
		IntentRoot:      intentRoot,
		ConfigDir:       configDir,
		ObjectModelRoot: orunRoot,
		Backend:         backend,
		Version:         version,
	}), nil
}

// runAgentTUI opens the cockpit straight onto the Agent surface — the bare
// `orun agent` front door (orun-agents-live AL3).
func runAgentTUI(ctx context.Context) error {
	svc, err := buildTUIService()
	if err != nil {
		return err
	}
	_, runErr := tui.NewProgramInAgentMode(svc).Run()
	return runErr
}

func runTUI(ctx context.Context) error {
	// Auto-discover the intent root so the cockpit (and the state/log store
	// it reads) resolves to the repo root regardless of which command path
	// launched it (`orun` vs `orun tui`) or which subdirectory we are in.
	// The tui command is not covered by the shared PersistentPreRunE
	// discovery, so we do it here, idempotently.
	if intentRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			if foundPath, foundDir, derr := discovery.FindIntentFile(cwd); derr == nil {
				intentFile = foundPath
				intentRoot = foundDir
			}
		}
	}

	backend, cleanup, err := resolveTUIBackend()
	if err != nil {
		return err
	}
	defer cleanup()

	// The local TUI reads/writes the content-addressed object graph under
	// .orun/objectmodel; only --remote-state attaches a backend.
	orunRoot, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return fmt.Errorf("resolve object-model root: %w", err)
	}

	svc := services.NewLiveOrunService(services.LiveServiceConfig{
		IntentFile:      intentFile,
		IntentRoot:      intentRoot,
		ConfigDir:       configDir,
		ObjectModelRoot: orunRoot,
		Backend:         backend,
		Version:         version,
	})

	p := tui.NewProgram(svc)
	_, runErr := p.Run()
	return runErr
}
