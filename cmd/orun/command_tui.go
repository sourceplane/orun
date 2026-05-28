package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/state"
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

// resolveTUIBackend constructs the appropriate state backend. It fails
// closed when --remote-state is set without a resolvable backend URL so
// no half-launched TUI ever runs with invalid remote config.
func resolveTUIBackend(store *state.Store) (statebackend.Backend, func(), error) {
	if !tuiRemoteState {
		return statebackend.NewFileStateBackend(store), func() {}, nil
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

func runTUI(ctx context.Context) error {
	store := state.NewStore(storeDir())

	backend, cleanup, err := resolveTUIBackend(store)
	if err != nil {
		return err
	}
	defer cleanup()

	svc := services.NewLiveOrunService(services.LiveServiceConfig{
		IntentFile: intentFile,
		IntentRoot: intentRoot,
		ConfigDir:  configDir,
		Store:      store,
		Backend:    backend,
		Version:    version,
	})

	p := tui.NewProgram(svc)
	_, runErr := p.Run()
	return runErr
}
