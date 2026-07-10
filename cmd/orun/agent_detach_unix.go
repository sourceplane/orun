//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

// detachBody re-execs this command as its own process group (the body),
// pinning the pre-minted session id, with output to a per-session log beside
// the registry entry. The parent prints the id and returns — the tmux
// discipline: the launcher is never the body's lifeline.
func detachBody(cmd *cobra.Command, sessionID string) error {
	liveDir := agentLiveDir()
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		return err
	}
	args := make([]string, 0, len(os.Args))
	for _, a := range os.Args[1:] {
		if a == "--detach" || a == "--detach=true" {
			continue
		}
		args = append(args, a)
	}
	args = append(args, "--session-id", sessionID)
	logPath := filepath.Join(liveDir, sessionID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	child := exec.Command(os.Args[0], args...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return fmt.Errorf("detach: %w", err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "session %s detached (pid %d, log %s)\n", sessionID, child.Process.Pid, logPath)
	fmt.Fprintf(out, "attach  orun agent attach %s\n", sessionID)
	return nil
}
