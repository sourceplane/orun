//go:build !unix

package procgroup

import "os/exec"

func configure(cmd *exec.Cmd) {}
