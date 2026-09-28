package catalogresolve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ManifestSetDigest hashes the authored inputs discovery reads for
// workspaceRoot: the root intent.yaml (inline components, catalog defaults and
// discovery excludes) and every component manifest discovery finds, as
// (workspace-relative path, raw bytes) pairs in discovery's sorted order. The
// result is a short hex digest, or "" when the workspace has neither an
// intent.yaml nor any component manifest.
//
// It is a cheap witness of "which component graph would a resolve produce": a
// manifest added, removed, moved or edited — including a component.yml, a
// gitignored manifest, or any edit in a workspace without git — moves the
// digest, where the git-derived source id alone does not. Callers fold it into
// the resolve-memo key so a persisted catalog built from an older manifest set
// is never served for the current working tree (OR3).
//
// The walk is the same one resolution performs (same excludes, same
// .yaml/.yml rules), without parsing the manifests. A discovery error (for
// example a directory holding both component.yaml and component.yml) is
// returned; a resolve over that workspace fails the same way.
func ManifestSetDigest(ctx context.Context, workspaceRoot string) (string, error) {
	if workspaceRoot == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", nil
	}

	h := sha256.New()
	found := false
	write := func(rel string, body []byte) {
		found = true
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(body))
		h.Write(body)
	}

	var excludes []string
	intentAbs := filepath.Join(root, "intent.yaml")
	if raw, rerr := os.ReadFile(intentAbs); rerr == nil {
		write("intent.yaml", raw)
		intent, lerr := loadIntent(intentAbs, "intent.yaml")
		if lerr != nil {
			return "", lerr
		}
		if intent != nil && intent.Catalog != nil && intent.Catalog.Discovery != nil {
			excludes = intent.Catalog.Discovery.Exclude
		}
	}

	rels, err := discover(ctx, root, excludes)
	if err != nil {
		return "", err
	}
	for _, rel := range rels {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			if os.IsNotExist(rerr) {
				continue // removed between the walk and the read
			}
			return "", rerr
		}
		write(rel, body)
	}
	if !found {
		return "", nil
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
