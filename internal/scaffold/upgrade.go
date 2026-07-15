package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/objectstore"
)

// UpgradeOptions drives a 3-way re-render of a scaffolded tree (design §11).
type UpgradeOptions struct {
	// TargetDir is the existing scaffolded tree (holds .orun/provenance.lock).
	TargetDir string
	// NewBlueprint is the newer blueprint document to re-render against the
	// recorded inputs. If nil, the blueprint pinned in the lock is re-used
	// (a no-op upgrade, useful to detect drift).
	NewBlueprint []byte
	// SourceBaseDir resolves relative `dir` source paths in the re-rendered
	// blueprint (the blueprint's own directory).
	SourceBaseDir string
	// Store holds the pinned old blueprint (by digest) needed to reconstruct
	// the base for the 3-way merge, and pins the new sources.
	Store objectstore.ObjectStore
	// Apply writes non-conflicting updates. When false, the upgrade is a
	// dry-run that only reports the merge plan.
	Apply bool
	// WorkDir is scratch for source materialization (temp if empty).
	WorkDir string
}

// FileMergeStatus classifies one file in the 3-way merge.
type FileMergeStatus string

const (
	// MergeUnchanged: new == base, nothing to do.
	MergeUnchanged FileMergeStatus = "unchanged"
	// MergeUpdated: blueprint-owned file the human did not touch (current ==
	// base) and new != base ⇒ safe to overwrite.
	MergeUpdated FileMergeStatus = "updated"
	// MergeCreated: file is new in this blueprint version.
	MergeCreated FileMergeStatus = "created"
	// MergeConflict: human edited a blueprint-owned file (current != base) and
	// the blueprint also changed it ⇒ surface, never overwrite (design §11).
	MergeConflict FileMergeStatus = "conflict"
)

// FileMerge is one file's merge decision.
type FileMerge struct {
	Path   string
	Status FileMergeStatus
}

// UpgradeResult is the reviewable merge plan.
type UpgradeResult struct {
	Merges  []FileMerge
	Applied bool
}

// Upgrade re-renders a newer blueprint against the recorded inputs and
// 3-way-merges into the target (design §11): the blueprint owns the files it
// produces; a file the human edited surfaces as a conflict rather than being
// overwritten. This is what makes an instance upgradable rather than a
// permanent fork.
func Upgrade(ctx context.Context, opts UpgradeOptions) (*UpgradeResult, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("upgrade: object store is required")
	}
	prov, err := ReadProvenance(opts.TargetDir)
	if err != nil {
		return nil, notFoundErr("upgrade: no provenance.lock in %q: %v", opts.TargetDir, err)
	}

	// Reconstruct the BASE by re-rendering the old blueprint (pinned in the
	// lock) with the recorded inputs.
	oldBP, err := fetchBlob(ctx, opts.Store, prov.Blueprint.Digest)
	if err != nil {
		return nil, notFoundErr("upgrade: old blueprint %s not in object store: %v", prov.Blueprint.Digest, err)
	}
	rawInputs := stringifyInputs(prov.Inputs)

	base, err := renderToMap(ctx, oldBP, rawInputs, opts.Store, opts.SourceBaseDir, opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("upgrade: reconstruct base: %w", err)
	}

	newBP := opts.NewBlueprint
	if newBP == nil {
		newBP = oldBP
	}
	next, err := renderToMap(ctx, newBP, rawInputs, opts.Store, opts.SourceBaseDir, opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("upgrade: render new: %w", err)
	}

	// Compute the merge plan over the union of base+next paths.
	paths := unionKeys(base, next)
	var merges []FileMerge
	for _, p := range paths {
		newBytes, inNew := next[p]
		baseBytes, inBase := base[p]
		current, onDisk := readTargetFile(opts.TargetDir, p)

		switch {
		case inNew && !inBase:
			merges = append(merges, FileMerge{Path: p, Status: MergeCreated})
		case inNew && inBase:
			if string(newBytes) == string(baseBytes) {
				merges = append(merges, FileMerge{Path: p, Status: MergeUnchanged})
				continue
			}
			// new != base: blueprint changed this file.
			if onDisk && string(current) != string(baseBytes) {
				merges = append(merges, FileMerge{Path: p, Status: MergeConflict})
			} else {
				merges = append(merges, FileMerge{Path: p, Status: MergeUpdated})
			}
		default:
			// present in base, removed in new — leave the human's copy alone.
			merges = append(merges, FileMerge{Path: p, Status: MergeUnchanged})
		}
	}
	sort.Slice(merges, func(i, j int) bool { return merges[i].Path < merges[j].Path })

	res := &UpgradeResult{Merges: merges}
	if opts.Apply {
		for _, m := range merges {
			if m.Status == MergeUpdated || m.Status == MergeCreated {
				target := filepath.Join(opts.TargetDir, filepath.FromSlash(m.Path))
				if !withinRoot(opts.TargetDir, target) {
					return nil, gateErr("upgrade: %q escapes target root", m.Path)
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(target, next[m.Path], 0o644); err != nil {
					return nil, err
				}
			}
		}
		res.Applied = true
	}
	return res, nil
}

// renderToMap runs the placement pipeline (no writes) and returns the placed
// files as a path→bytes map — the reusable core of both scaffold and upgrade.
func renderToMap(ctx context.Context, rawBlueprint []byte, rawInputs map[string]string, store objectstore.ObjectStore, baseDir, workDir string) (map[string][]byte, error) {
	bp, err := ParseBlueprint(rawBlueprint)
	if err != nil {
		return nil, err
	}
	values, err := CollectInputs(bp.Inputs, rawInputs)
	if err != nil {
		return nil, err
	}
	wd := workDir
	if wd == "" {
		wd, err = os.MkdirTemp("", "orun-upgrade-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(wd)
	}
	sources, err := resolveSources(ctx, store, bp.Sources, bp.Ignore, baseDir, wd)
	if err != nil {
		return nil, err
	}
	order, err := orderModules(bp)
	if err != nil {
		return nil, err
	}
	byName := map[string]Module{}
	for _, m := range bp.Modules {
		byName[m.Name] = m
	}
	out := map[string][]byte{}
	for _, batch := range order {
		for _, name := range batch {
			m := byName[name]
			var tree FileTree
			if m.Source != "" {
				tree = sources[m.Source].Tree
			}
			placed, err := placeModule(m, tree, values)
			if err != nil {
				return nil, err
			}
			for _, f := range placed.files {
				out[f.Path] = f.Bytes
			}
		}
	}
	return out, nil
}

func fetchBlob(ctx context.Context, store objectstore.ObjectStore, digest string) ([]byte, error) {
	_, data, err := store.Get(ctx, objectstore.ObjectID(digest))
	return data, err
}

func stringifyInputs(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		// A redacted secret ("<secret>") re-collects to the same placeholder;
		// the secret sweep still forbids it surviving into output.
		out[k] = fmt.Sprint(v)
	}
	return out
}

func readTargetFile(dir, rel string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, false
	}
	return data, true
}

func unionKeys(a, b map[string][]byte) []string {
	set := map[string]struct{}{}
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
