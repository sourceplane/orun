package affected

import (
	"context"
	"path"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// FingerprintChangeSource is the content-aware ChangeSource: the virtual Merkle
// tree (data-model.md §3). It recomputes each component's *current* input
// fingerprint and compares the subtree hash to the one the catalog recorded at
// resolve time — a component whose subtree differs has changed. Unlike git diff
// it is content-identity-aware (a comment-only edit that doesn't change the
// resolved inputs is not a change: early cutoff), so it is the cockpit's
// live/dirty change source where re-shelling git every tick is undesirable.
//
// It yields the changed component directories as "files": the engine's ownership
// map maps each dir back to its component, feeding the identical downstream
// pipeline as the git source.
type FingerprintChangeSource struct {
	Catalog       *objcatalog.CatalogView
	WorkspaceRoot string // absolute workspace root
	IntentPath    string // intent path (absolute, or relative to WorkspaceRoot); defaults to <root>/intent.yaml
}

// ChangedPaths recomputes fingerprints and returns the changed component dirs
// plus the intent signal (a changed shared global digest ⇒ intent changed,
// reported as undiffable/global since the source has no base/head bytes).
func (s FingerprintChangeSource) ChangedPaths(ctx context.Context) ([]string, IntentChange, error) {
	if s.Catalog == nil {
		return nil, IntentChange{}, nil
	}

	currentGlobal := catalogresolve.ComputeGlobalDigest(s.intentAbs())

	var changed []string
	var storedGlobal string
	haveStoredGlobal := false
	for _, c := range s.Catalog.Components {
		if c.Path == "" {
			continue
		}
		dir := path.Dir(c.Path)
		stored, ok := s.Catalog.Fingerprints[c.ComponentKey]
		if ok && !haveStoredGlobal {
			storedGlobal = stored.GlobalDigest
			haveStoredGlobal = true
		}
		// Recompute the subtree using the STORED global digest so a global
		// (intent) change does not masquerade as every component changing —
		// that is reported once via the intent signal below.
		gd := currentGlobal
		if ok {
			gd = stored.GlobalDigest
		}
		cur := catalogresolve.FingerprintForDir(s.WorkspaceRoot, dir, c.ComponentKey, gd)
		if !ok || cur.Subtree != stored.Subtree {
			changed = append(changed, dir)
		}
	}

	intent := IntentChange{}
	if haveStoredGlobal && currentGlobal != storedGlobal {
		intent.Changed = true // base/head bytes unavailable → engine treats as global
	}
	return changed, intent, nil
}

func (s FingerprintChangeSource) intentAbs() string {
	if s.IntentPath == "" {
		return filepath.Join(s.WorkspaceRoot, "intent.yaml")
	}
	if filepath.IsAbs(s.IntentPath) {
		return s.IntentPath
	}
	return filepath.Join(s.WorkspaceRoot, filepath.FromSlash(s.IntentPath))
}
