package workflowbackend

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// WorkflowDigest computes the content digest of a workflow file — the value a
// plan step or a provenance hook pins so the *reference*, not the *outcome*, is
// the durable state (design §5/§7). The returned string uses the same
// "sha256:<hex>" shape as the object store's content ids.
//
// v1 hashes the workflow file bytes. Extending the hash to fold in the referenced
// action-store module manifests (so a provider change also flips the digest) is a
// declared follow-on that keeps the same shape.
func WorkflowDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read workflow %q: %w", path, err)
	}
	return DigestBytes(data), nil
}

// DigestBytes is WorkflowDigest over in-memory bytes. Deterministic: identical
// bytes always yield an identical digest, so a workflow reference folds into a
// plan checksum without folding in any runtime outcome.
func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}
