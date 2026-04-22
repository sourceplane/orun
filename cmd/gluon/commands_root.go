package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	cliName         = "gluon"
	configDirEnvVar = "GLUON_CONFIG_DIR"
	runnerEnvVar    = "GLUON_RUNNER"
)

var version = "dev"

var (
	intentFile   string
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

func init() {
	rootCmd.SetVersionTemplate(cliName + " version {{.Version}}\n")
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", fmt.Sprintf("Config directory for JobRegistry definitions (or set %s; use * or ** for recursive scanning)", configDirEnvVar))

	registerPlanCommand(rootCmd)
	registerRunCommand(rootCmd)
	registerValidateCommand(rootCmd)
	registerDebugCommand(rootCmd)
	registerCompositionsCommand(rootCmd)
	registerComponentCommand(rootCmd)
}
