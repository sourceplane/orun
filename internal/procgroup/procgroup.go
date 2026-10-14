// Package procgroup makes a step's process tree die with the step. A step
// is `sh -c <script>`: killing the shell alone on a cancelled context leaves
// its children running and, because they hold the output pipes, cmd.Wait
// blocks until the last of them exits — a drained run waited for a `sleep`
// to finish (specs/orun-runner-efficiency §5.6). Configure puts the command
// in a process group of its own, kills the whole group when its context is
// cancelled, and bounds how long Wait lingers on the pipes afterwards.
package procgroup

import (
	"os/exec"
	"time"
)

// WaitDelay bounds how long a cancelled command's Wait blocks on output
// pipes held by descendants the kill did not reach.
const WaitDelay = 5 * time.Second

// Configure applies the process-group policy to cmd, which must have been
// created with exec.CommandContext for the cancel to apply.
func Configure(cmd *exec.Cmd) {
	configure(cmd)
	cmd.WaitDelay = WaitDelay
}
