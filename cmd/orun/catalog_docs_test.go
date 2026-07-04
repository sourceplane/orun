package main

// WO3.1c — `catalog docs`. Unit tests for the doc_ref extraction (docBlobRef);
// the store read + render is exercised end to end against a real catalog closure
// during development (see the PR notes) — here we pin the branching that decides
// whether a named doc has printable content.

import "testing"

func TestDocBlobRef_DocRefWithDigest(t *testing.T) {
	docs := map[string]any{"overview": map[string]any{
		"path": "docs/overview.md", "sha": "ae6ab3", "digest": "sha256:7b0b2a"}}
	digest, path, sha, err := docBlobRef(docs, "Repo", "ns/repo/ogpic", "overview")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if digest != "sha256:7b0b2a" || path != "docs/overview.md" || sha != "ae6ab3" {
		t.Errorf("got digest=%q path=%q sha=%q", digest, path, sha)
	}
}

func TestDocBlobRef_MissingDocExit6(t *testing.T) {
	_, _, _, err := docBlobRef(map[string]any{}, "Repo", "ns/repo/ogpic", "overview")
	if got := exitCodeOf(t, err); got != 6 {
		t.Errorf("exit = %d, want 6", got)
	}
}

func TestDocBlobRef_PathPointerOnlyExit6(t *testing.T) {
	// A bare string (a component's docs.overview) has no closure blob.
	docs := map[string]any{"overview": "overview.md"}
	_, _, _, err := docBlobRef(docs, "Component", "ns/repo/api-edge", "overview")
	if got := exitCodeOf(t, err); got != 6 {
		t.Errorf("exit = %d, want 6", got)
	}
}

func TestDocBlobRef_NoDigestExit6(t *testing.T) {
	// A doc_ref map that never got its digest stamped (path pointer only).
	docs := map[string]any{"overview": map[string]any{"path": "docs/overview.md"}}
	_, _, _, err := docBlobRef(docs, "Repo", "ns/repo/ogpic", "overview")
	if got := exitCodeOf(t, err); got != 6 {
		t.Errorf("exit = %d, want 6", got)
	}
}
