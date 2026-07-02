package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	cliName         = "orun"
	configDirEnvVar = "ORUN_CONFIG_DIR"
	runnerEnvVar    = "ORUN_RUNNER"
	execIDEnvVar    = "ORUN_EXEC_ID"
	planIDEnvVar    = "ORUN_PLAN_ID"
	noColorEnvVar   = "ORUN_NO_COLOR"
	dryRunEnvVar    = "ORUN_DRY_RUN"
	stateDirEnvVar  = "ORUN_STATE_DIR"
	// noTUIEnvVar, when truthy, makes a bare `orun` invocation print help
	// instead of opening the cockpit TUI — an escape hatch for scripts and
	// users who prefer the classic default.
	noTUIEnvVar = "ORUN_NO_TUI"
	// Remote state env vars
	remoteStateEnvVar = "ORUN_REMOTE_STATE"
	backendURLEnvVar  = "ORUN_BACKEND_URL"
	tokenEnvVar       = "ORUN_TOKEN"
	// Org/project scope env vars (CI scoping; design §8 precedence).
	// workspaceEnvVar is the leading spelling; orgEnvVar is the retained alias
	// (read either, prefer workspace — saas-workspaces A4).
	workspaceEnvVar = "ORUN_WORKSPACE"
	orgEnvVar       = "ORUN_ORG"
	projectEnvVar   = "ORUN_PROJECT"
)

var version = "dev"

var (
	intentFile               string
	intentRoot               string
	allFlag                  bool
	configDir                string
	outputFile               string
	outputFormat             string
	debugMode                bool
	environment              string
	allEnvs                  bool
	longFormat               bool
	expandJobs               bool
	viewPlan                 string
	changedOnly              bool
	baseBranch               string
	headRef                  string
	changedFiles             []string
	uncommitted              bool
	untracked                bool
	compositionPackageRoot   string
	compositionPackageOutput string
	explainChanged           bool
	intentImpact             string
	triggerName              string
	fromCI                   string
	eventFile                string
)

var rootCmd = &cobra.Command{
	Use:   cliName,
	Short: "Plan and run changes from intent",
	Long: "orun turns intent into deterministic plans and runs them with clear, resumable execution feedback.\n\n" +
		"Run `orun` with no arguments in an interactive terminal to open the Cockpit TUI; " +
		"set ORUN_NO_TUI=1 (or run in a non-interactive shell) to print help instead.",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
	// A bare `orun` opens the cockpit TUI on an interactive terminal and
	// otherwise prints help. NoArgs keeps `orun <typo>` reporting an unknown
	// command rather than being swallowed by this handler.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if shouldLaunchDefaultTUI() {
			return runTUI(cmd.Context())
		}
		return cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if commandNeedsConfig(cmd) && configDir == "" {
			if envConfigDir := configDirEnvValue(); envConfigDir != "" {
				configDir = envConfigDir
			}
		}

		// Intent auto-discovery: walk up directory tree to find intent.yaml.
		// The auth/cloud groups also resolve the backend URL from
		// intent.yaml execution.state, so they opt in via
		// commandResolvesCloudConfig without being treated as catalog commands.
		if !cmd.Flags().Changed("intent") && (commandUsesIntent(cmd) || commandResolvesCloudConfig(cmd)) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil
			}
			foundPath, foundDir, err := discovery.FindIntentFile(cwd)
			if err == nil {
				intentFile = foundPath
				intentRoot = foundDir
			}
		} else if cmd.Flags().Changed("intent") {
			intentRoot = filepath.Dir(intentFile)
			if !filepath.IsAbs(intentRoot) {
				if cwd, err := os.Getwd(); err == nil {
					intentRoot = filepath.Join(cwd, intentRoot)
				}
			}
		}

		// Universal refresh hook (§0): keep catalogs/current fresh as a
		// side effect of using orun. Best-effort and non-fatal — never blocks
		// or fails the command's primary work.
		maybeAutoRefresh(cmd)

		return nil
	},
}

func envValue(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// shouldLaunchDefaultTUI reports whether a bare `orun` invocation should
// open the cockpit. It launches only on a real interactive terminal (both
// stdin and stdout are TTYs) and never when ORUN_NO_TUI is set truthy — so
// CI, pipes, and redirected output fall back to printing help, preserving
// scriptable behavior.
func shouldLaunchDefaultTUI() bool {
	switch os.Getenv(noTUIEnvVar) {
	case "", "0", "false", "no":
		// fall through — TUI allowed
	default:
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func configDirEnvValue() string {
	return envValue(configDirEnvVar)
}

func runnerEnvValue() string {
	return envValue(runnerEnvVar)
}

func commandNeedsConfig(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	if cmd.Name() == "plan" || cmd.Name() == "compositions" || cmd.Name() == "composition" {
		return true
	}

	if parent := cmd.Parent(); parent != nil {
		if parent.Name() == "compositions" || parent.Name() == "composition" {
			return true
		}
	}

	return false
}

// storeDir returns the directory to use for the .orun/ state store.
// When an intent file has been located, the store lives at the intent root
// (i.e. the repo root), not at the current working directory.
func storeDir() string {
	if intentRoot != "" {
		return intentRoot
	}
	return "."
}

// newRemoteBackend creates a RemoteStateBackend using the resolved token source.
// The org/project scope is resolved from ORUN_ORG/ORUN_PROJECT and the cached
// RepoLink (these read paths expose no --org/--project flags yet); empty fields
// default to the OSS single-tenant _local scope inside the client.
func newRemoteBackend(backendURL string) (statebackend.Backend, error) {
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{
		BackendURL:  backendURL,
		Version:     version,
		Interactive: false,
	})
	if err != nil {
		return nil, fmt.Errorf("remote state auth: %w", err)
	}
	linkOrg, linkProject := "", ""
	if repo, repoErr := resolveRepoContext(backendURL); repoErr == nil && repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	// Honor the declared intent org/project (flag/env unavailable on read paths;
	// intent sits above the cached link — specs/oidc-ci-tenancy §4.1).
	intentOrg, intentProject, _ := intentScope(loadIntentForCloudConfig())
	scope := resolveScope("", "", intentOrg, intentProject, linkOrg, linkProject)
	client := remotestate.NewClientWithScope(backendURL, version, tokenSrc, scope)
	runnerID := statebackend.DeriveRunnerID()
	return statebackend.NewRemoteStateBackend(client, runnerID), nil
}

func commandUsesIntent(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Name() {
	case "plan", "run", "validate", "debug", "component", "compositions", "get", "describe", "status", "logs", "intent":
		return true
	}
	if parent := cmd.Parent(); parent != nil {
		return commandUsesIntent(parent)
	}
	return false
}

// commandResolvesCloudConfig reports whether cmd resolves the backend/auth
// endpoint from intent.yaml execution.state: the auth and cloud command groups.
// It is deliberately separate from commandUsesIntent so these commands get
// intent-based backend-URL discovery without being treated as object-catalog
// commands (which would trigger the universal auto-refresh hook).
func commandResolvesCloudConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "auth", "cloud":
			return true
		}
	}
	return false
}

func init() {
	rootCmd.SetVersionTemplate(cliName + " version {{.Version}}\n")
	rootCmd.PersistentFlags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path (auto-discovered if not set)")
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", fmt.Sprintf("Config directory for JobRegistry definitions (or set %s; use * or ** for recursive scanning)", configDirEnvVar))
	rootCmd.PersistentFlags().BoolVar(&allFlag, "all", false, "Disable CWD-based component scoping; process all components")

	registerPlanCommand(rootCmd)
	registerRunCommand(rootCmd)
	registerAuthCommand(rootCmd)
	registerCloudCommand(rootCmd)
	registerBackendCommand(rootCmd)
	registerValidateCommand(rootCmd)
	registerDebugCommand(rootCmd)
	registerCompositionsCommand(rootCmd)
	registerComponentCommand(rootCmd)
	registerStatusCommand(rootCmd)
	registerLogsCommand(rootCmd)
	registerGetCommand(rootCmd)
	registerDescribeCommand(rootCmd)
	registerGCCommand(rootCmd)
	registerPublishCommand(rootCmd)
	registerFetchCommand(rootCmd)
	registerIntentCommand(rootCmd)
	registerGithubCommand(rootCmd)
	registerTuiCommand(rootCmd)
	registerCatalogCommand(rootCmd)
	registerObjectsCommand(rootCmd)
	registerSecretsCommand(rootCmd)
}
