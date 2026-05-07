package harness_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHarnessDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dry-run guard requires bash")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path")
	}
	guard := filepath.Join(filepath.Dir(file), "dry-run-guard.sh")
	cmd := exec.Command("bash", guard)
	out, err := cmd.CombinedOutput()
	t.Logf("dry-run guard output:\n%s", out)
	if err != nil {
		t.Fatalf("dry-run guard failed: %v", err)
	}
}
