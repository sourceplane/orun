//go:build !unix

package preview

import (
	"os"
	"os/exec"
)

func detach(cmd *exec.Cmd) {}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

func groupAlive(pid int) bool { return alive(pid) }

func signalGroup(pid int, kill bool) {
	if pid <= 1 || pid == os.Getpid() {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
