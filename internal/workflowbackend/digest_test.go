package workflowbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDigestBytesDeterministic(t *testing.T) {
	a := DigestBytes([]byte("apiVersion: torkflow/v1\n"))
	b := DigestBytes([]byte("apiVersion: torkflow/v1\n"))
	if a != b {
		t.Fatalf("identical bytes produced different digests: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("digest missing sha256: prefix: %s", a)
	}
	if DigestBytes([]byte("different")) == a {
		t.Fatalf("different bytes produced identical digest")
	}
}

func TestWorkflowDigestStableAndSensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.yaml")
	body := []byte("apiVersion: torkflow/v1\nkind: Workflow\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := WorkflowDigest(path)
	if err != nil {
		t.Fatalf("WorkflowDigest: %v", err)
	}
	if want := DigestBytes(body); got != want {
		t.Fatalf("WorkflowDigest(%s)=%s, want %s", path, got, want)
	}

	// Re-reading the unchanged file is byte-identical.
	again, err := WorkflowDigest(path)
	if err != nil || again != got {
		t.Fatalf("WorkflowDigest not stable: %s vs %s (err %v)", got, again, err)
	}

	// A content change flips the digest.
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := WorkflowDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed == got {
		t.Fatalf("digest did not change after content change")
	}
}

func TestWorkflowDigestMissingFile(t *testing.T) {
	if _, err := WorkflowDigest(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatalf("expected error for missing workflow file")
	}
}
