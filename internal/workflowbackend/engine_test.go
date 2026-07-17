package workflowbackend

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeEngine writes an executable shell script that acts as a pinned engine
// over the JSON contract, and returns its path. Skips on non-POSIX platforms.
func writeFakeEngine(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake engine uses /bin/sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveEngineErrors(t *testing.T) {
	// Unconfigured (no Bin, env cleared) is a clear error.
	t.Setenv(EngineEnv, "")
	if _, err := ResolveEngine(EngineOptions{}); err == nil {
		t.Fatalf("expected error when engine is unconfigured")
	}
	// A configured-but-missing binary is a clear error, not a panic.
	if _, err := ResolveEngine(EngineOptions{Bin: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatalf("expected error for missing engine binary")
	}
}

func TestResolveEngineDigestFromEnv(t *testing.T) {
	bin := writeFakeEngine(t, "true\n")
	t.Setenv(EngineEnv, bin)
	eng, err := ResolveEngine(EngineOptions{})
	if err != nil {
		t.Fatalf("ResolveEngine: %v", err)
	}
	if eng.Digest() == "" || eng.Bin != bin {
		t.Fatalf("engine not resolved from env: digest=%q bin=%q", eng.Digest(), eng.Bin)
	}
	// The engine digest is the content digest of the binary.
	if want := mustDigest(t, bin); eng.Digest() != want {
		t.Fatalf("engine digest=%s want %s", eng.Digest(), want)
	}
}

func TestInvokeRoundTrip(t *testing.T) {
	// Echo the JSON request back inside the result outputs, proving stdin
	// delivery and stdout parsing across the process boundary.
	bin := writeFakeEngine(t, `printf '{"contract":"v1","status":"success","outputs":{"got":'; cat; printf '}}'`)
	eng, err := ResolveEngine(EngineOptions{Bin: bin, Args: []string{}})
	if err != nil {
		t.Fatalf("ResolveEngine: %v", err)
	}

	res, err := eng.Invoke(context.Background(), Request{
		Workflow: "workflows/notify.yaml",
		With:     map[string]any{"channel": "ops"},
		Metadata: map[string]any{"jobId": "web@prod.deploy"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !res.Succeeded() {
		t.Fatalf("expected success, got status %q", res.Status)
	}
	got, ok := res.Outputs["got"].(map[string]any)
	if !ok {
		t.Fatalf("outputs.got missing or wrong type: %#v", res.Outputs)
	}
	if got["workflow"] != "workflows/notify.yaml" {
		t.Fatalf("request did not reach the engine: %#v", got)
	}
	if got["contract"] != "v1" {
		t.Fatalf("request was not stamped contract/v1: %#v", got)
	}
}

func TestInvokeWorkflowFailureIsNotAnError(t *testing.T) {
	// A workflow that runs and fails: non-zero exit, but a structured Result.
	bin := writeFakeEngine(t, `echo '{"status":"failed","error":"boom"}'; exit 1`)
	eng, err := ResolveEngine(EngineOptions{Bin: bin, Args: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Invoke(context.Background(), Request{Workflow: "wf.yaml"})
	if err != nil {
		t.Fatalf("workflow failure should not be an infrastructure error: %v", err)
	}
	if res.Succeeded() || res.Error != "boom" {
		t.Fatalf("expected failed result with error boom, got %#v", res)
	}
}

func TestInvokeInvalidOutputIsError(t *testing.T) {
	bin := writeFakeEngine(t, `echo 'not json'`)
	eng, err := ResolveEngine(EngineOptions{Bin: bin, Args: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Invoke(context.Background(), Request{Workflow: "wf.yaml"}); err == nil {
		t.Fatalf("expected error for invalid engine output")
	}
}

func mustDigest(t *testing.T, path string) string {
	t.Helper()
	d, err := WorkflowDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
