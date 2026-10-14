//go:build unix

package procgroup

import (
	"os/exec"
	"syscall"
)

func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// The whole group: the shell and every process it started.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
