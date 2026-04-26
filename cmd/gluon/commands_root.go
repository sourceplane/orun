package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/gluon/internal/discovery"
	"github.com/spf13/cobra"
)

const (
	cliName         = "gluon"
	configDirEnvVar = "GLUON_CONFIG_DIR"
	runnerEnvVar    = "GLUON_RUNNER"
	execIDEnvVar    = "GLUON_EXEC_ID"
	planIDEnvVar    = "GLUON_PLAN_ID"
	noColorEnvVar   = "GLUON_NO_COLOR"
	dryRunEnvVar    = "GLUON_DRY_RUN"
	stateDirEnvVar  = "GLUON_STATE_DIR"
)

var version = "dev"

var (
	intentFile   string
	intentRoot   string
	allFlag      bool
	configDir    string
	outputFile   string
	outputFormat string
	debugMode    bool
	environment  string
	longFormat   bool
	expandJobs   bool
	viewPlan     string
	changedOnly  bool
	baseBranch   string
	headRef      string
	changedFiles []string
	uncommitted  bool
	untracked    bool
	compositionPackageRoot   string
	compositionPackageOutput string
)

var rootCmd = &cobra.Command{
	Use:     cliName,
	Short:   "Planner engine: Intent → Plan DAG",
	Long:    "gluon is a schema-driven planner that compiles policy-aware intent into deterministic execution DAGs",
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if commandNeedsConfig(cmd) && configDir == "" {
			if envConfigDir := configDirEnvValue(); envConfigDir != "" {
				configDir = envConfigDir
			}
		}

		// Intent auto-discovery: walk up directory tree to find intent.yaml
		if !cmd.Flags().Changed("intent") && commandUsesIntent(cmd) {
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

func commandUsesIntent(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Name() {
	case "plan", "run", "validate", "debug", "component", "compositions", "get", "describe", "status", "logs":
		return true
	}
	if parent := cmd.Parent(); parent != nil {
		return commandUsesIntent(parent)
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
	registerValidateCommand(rootCmd)
	registerDebugCommand(rootCmd)
	registerCompositionsCommand(rootCmd)
	registerComponentCommand(rootCmd)
	registerStatusCommand(rootCmd)
	registerLogsCommand(rootCmd)
	registerGetCommand(rootCmd)
	registerDescribeCommand(rootCmd)
	registerGCCommand(rootCmd)
}
