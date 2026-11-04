//go:build unix

package preview

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// detach puts the process in a session of its own, so it outlives the
// command that started it and its group can be signalled whole.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// alive reports whether pid is a live process. A zombie is not: in a box
// nothing may reap a server whose starter has exited.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		// pid (comm) S …: the state follows the last ')'.
		if i := bytes.LastIndexByte(b, ')'); i >= 0 && i+2 < len(b) && b[i+2] == 'Z' {
			return false
		}
	}
	return true
}

// groupAlive reports whether any process is left in pid's group.
func groupAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// signalGroup signals pid's process group, or pid alone when it leads none.
func signalGroup(pid int, kill bool) {
	if pid <= 1 || pid == os.Getpid() {
		return
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}
