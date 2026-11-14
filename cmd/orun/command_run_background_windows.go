//go:build windows

package main

import "syscall"

// backgroundSysProcAttr starts the detached run in a new process group so a
// Ctrl+C in the launching console does not reach it.
func backgroundSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
