package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/gluon/internal/state"
	"github.com/spf13/cobra"
)

var (
	logsExecID string
	logsJob    string
	logsStep   string
)

var logsCmd = &cobra.Command{
	Use:   "logs [run/<exec-id>] [job/<job-id>]",
	Short: "Show execution logs",
	Long:  "Show logs from an execution. Supports resource slash notation: logs run/<id>, logs job/<id>.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse resource slash notation from args
		for _, arg := range args {
			if strings.HasPrefix(arg, "run/") {
				logsExecID = strings.TrimPrefix(arg, "run/")
			} else if strings.HasPrefix(arg, "job/") {
				logsJob = strings.TrimPrefix(arg, "job/")
			}
		}
		return showLogs()
	},
}

func registerLogsCommand(root *cobra.Command) {
	root.AddCommand(logsCmd)

	logsCmd.Flags().StringVar(&logsExecID, "exec-id", "", "Execution ID")
	logsCmd.Flags().StringVar(&logsJob, "job", "", "Filter logs by job ID")
	logsCmd.Flags().StringVar(&logsStep, "step", "", "Filter logs by step ID")
}

func showLogs() error {
	store := state.NewStore(storeDir())

	execID := logsExecID
	if execID == "" {
		var err error
		execID, err = store.ResolveExecID("latest")
		if err != nil {
			fmt.Println("No executions found.")
			return nil
		}
	} else {
		var err error
		execID, err = store.ResolveExecID(execID)
		if err != nil {
			return err
		}
	}

	logsBase := filepath.Join(store.ExecPath(execID), "logs")
	if _, err := os.Stat(logsBase); os.IsNotExist(err) {
		fmt.Printf("No logs found for execution %s\n", execID)
		return nil
	}

	// Walk log files
	return filepath.Walk(logsBase, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".log") {
			return nil
		}

		rel, _ := filepath.Rel(logsBase, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)

		jobID := ""
		stepID := ""
		if len(parts) >= 1 {
			jobID = parts[0]
		}
		if len(parts) >= 2 {
			stepID = strings.TrimSuffix(parts[1], ".log")
		}

		// Apply filters
		if logsJob != "" && !strings.Contains(jobID, logsJob) {
			return nil
		}
		if logsStep != "" && !strings.Contains(stepID, logsStep) {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			return nil
		}

		fmt.Fprintf(os.Stdout, "─── %s / %s ───\n", jobID, stepID)
		fmt.Fprintln(os.Stdout, content)
		fmt.Fprintln(os.Stdout)

		return nil
	})
}
