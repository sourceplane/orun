package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
)

// backgroundChildEnv flags a child process spawned by --background so it can
// avoid re-spawning itself recursively.
const backgroundChildEnv = "GLUON_BACKGROUND_CHILD"

// startBackgroundRun re-execs the current binary with the same args minus the
// --background flag, redirects its stdout/stderr to a per-execution log file,
// and detaches it into its own process group so the parent can exit cleanly.
//
// The caller is responsible for resolving the exec ID before calling this so
// the user knows which run to track via `gluon status --exec-id <id>`.
func startBackgroundRun(execID string, store *state.Store, color bool) error {
	if execID == "" {
		return fmt.Errorf("background mode requires a resolved exec id")
	}

	// Ensure the execution directory exists so we can write the run log.
	execDir := store.ExecPath(execID)
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		return fmt.Errorf("create execution directory: %w", err)
	}

	logPath := filepath.Join(execDir, "run.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open run log %s: %w", logPath, err)
	}

	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate gluon binary: %w", err)
	}

	args := buildBackgroundChildArgs(os.Args[1:], execID)

	cmd := exec.Command(binary, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(),
		backgroundChildEnv+"=1",
		"GLUON_EXEC_ID="+execID,
		"GLUON_NO_COLOR=1",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start background gluon: %w", err)
	}
	pid := cmd.Process.Pid
	// Closing our handle does not affect the child since it has its own fd.
	_ = logFile.Close()

	// Persist the pid so `gluon status` can show it.
	pidPath := filepath.Join(execDir, "pid")
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o644)

	// Release the child so it isn't reaped by us if we somehow stick around.
	_ = cmd.Process.Release()

	fmt.Fprintf(os.Stdout, "%s background run started\n",
		ui.Green(color, "✓"))
	fmt.Fprintf(os.Stdout, "  %s %s\n", ui.Dim(color, "exec-id:"), execID)
	fmt.Fprintf(os.Stdout, "  %s %d\n", ui.Dim(color, "pid:    "), pid)
	fmt.Fprintf(os.Stdout, "  %s %s\n", ui.Dim(color, "log:    "), logPath)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s gluon status --exec-id %s --watch\n",
		ui.Dim(color, "track: "), execID)
	fmt.Fprintf(os.Stdout, "  %s gluon logs   --exec-id %s\n",
		ui.Dim(color, "logs:  "), execID)
	return nil
}

// buildBackgroundChildArgs strips --background / --bg and any --exec-id the
// user passed, then appends a fresh --exec-id so the child writes state under
// the same id the parent printed.
func buildBackgroundChildArgs(args []string, execID string) []string {
	out := make([]string, 0, len(args)+2)
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		switch {
		case a == "--background", a == "--bg":
			continue
		case a == "--exec-id":
			skipNext = true
			continue
		case strings.HasPrefix(a, "--exec-id="):
			continue
		default:
			out = append(out, a)
		}
	}
	out = append(out, "--exec-id", execID)
	return out
}

// isBackgroundChild reports whether the current process was spawned as the
// detached child of a `--background` invocation.
func isBackgroundChild() bool {
	return strings.TrimSpace(os.Getenv(backgroundChildEnv)) != ""
}
