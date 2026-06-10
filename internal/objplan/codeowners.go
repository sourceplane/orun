package objplan

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

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
// CODEOWNERS file the owner resolver reads and the composition lock the
// composition resolver reads — into a short hex digest. The resolve memo folds
// it into its key so a change to either file (which feeds the resolved catalog
// but may be untracked, like a gitignored lock) can never serve a stale
// memoized catalog. Returns "" when none of the files exist.
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
