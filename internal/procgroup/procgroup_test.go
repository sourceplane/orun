package procgroup

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Cancelling the context must end a shell step whose child outlives the
// shell: the group is killed and Wait does not block on the child's pipes.
func TestConfigure_KillsTheWholeGroupOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30")
	Configure(cmd)
	var out []byte
	done := make(chan error, 1)
	go func() {
		var err error
		out, err = cmd.CombinedOutput()
		done <- err
	}()
	time.Sleep(150 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled step fails")
		}
		if el := time.Since(start); el > 3*time.Second {
			t.Fatalf("the step's child kept the step alive for %v after cancel", el)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the step never returned: the child held the pipes")
	}
	_ = out
}
