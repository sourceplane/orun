package objplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/codeowners"
)

// codeownersLocations are the conventional CODEOWNERS file locations, in
// GitHub's documented precedence (.github/ first, then root, then docs/;
// first found wins).
var codeownersLocations = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

// WorkspaceInputsDigest hashes the extra-source resolver inputs — the
// CODEOWNERS file the owner resolver reads, the composition lock the
// composition resolver reads, and the authored component-manifest set
// (catalogresolve.ManifestSetDigest: intent.yaml plus every discovered
// component.yaml/.yml) — into a short hex digest. The resolve memo folds it
// into its key so a change to any of them (which feed the resolved catalog but
// may be invisible to the git-derived source id: an untracked lock, a
// gitignored or component.yml manifest, any edit in a workspace without git)
// can never serve a stale memoized catalog. Returns "" when none of the inputs
// exist.
//
// Note the by-commit provenance property (the epic's defining property) holds
// fully only when these files are committed; the lockfile convention is to
// commit it (the root .gitignore un-ignores /.orun/compositions.lock.yaml).
// This digest is the safety net for workspaces that don't.
func WorkspaceInputsDigest(root string) string {
	if root == "" {
		return ""
	}
	h := sha256.New()
	any := false
	paths := append(append([]string(nil), codeownersLocations...), compositionLockPath)
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		any = true
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	// The component-manifest set (OR3): without it the memo, keyed on the
	// source id, would keep serving a catalog built from an older manifest set
	// whenever the source id cannot see the change. A discovery error is folded
	// in as-is so the memo misses and the resolve reports it.
	manifests, merr := catalogresolve.ManifestSetDigest(context.Background(), root)
	if merr != nil {
		manifests = "error:" + merr.Error()
	}
	if manifests != "" {
		any = true
		h.Write([]byte("manifests"))
		h.Write([]byte{0})
		h.Write([]byte(manifests))
		h.Write([]byte{0})
	}
	if !any {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// OwnerResolverForWorkspace reads the workspace's CODEOWNERS file (if any) and
// returns an OwnerResolver over it, or nil when no CODEOWNERS exists. Every
// catalog-building entry point derives the resolver this way so the resolved
// ownership — and therefore the catalog content id — is identical regardless of
// which path (refresh/plan/seam) produced the catalog for a given source.
func OwnerResolverForWorkspace(root string) OwnerResolver {
	if root == "" {
		return nil
	}
	for _, loc := range codeownersLocations {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(loc)))
		if err != nil {
			continue
		}
		rs := codeowners.Parse(b)
		if rs.Empty() {
			return nil
		}
		return rs.Owners
	}
	return nil
}
