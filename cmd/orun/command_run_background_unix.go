//go:build unix

package main

import "syscall"

// backgroundSysProcAttr puts the detached run in its own process group so a
// signal to the launching terminal does not reach it.
func backgroundSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
