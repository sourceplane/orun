package objplan

import (
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/codeowners"
)

// codeownersLocations are the conventional CODEOWNERS file locations, in the
// precedence GitHub uses (first found wins).
var codeownersLocations = []string{
	"CODEOWNERS",
	".github/CODEOWNERS",
	"docs/CODEOWNERS",
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
