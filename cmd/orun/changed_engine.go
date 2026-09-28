package main

// changed_engine.go routes `plan`/`run --changed` through the unified
// change-detection engine (internal/affected) over the object-model catalog.
//
// Per the catalog-state design, the object catalog is always the FULL catalog;
// selecting the --changed subset is a plan/run-time duty. This helper refreshes
// the catalog if the source or the component manifests changed (cheap memo hit
// otherwise, see changedCatalogView), loads the full catalog, runs the engine, and returns the selected component *names* — the
// same Selection (DirectlyChanged ∪ include:always closure) the golden parity
// gate (changed_parity_test.go) locks.
//
// This is the single --changed selection path: the legacy file-walking selector
// was retired in CS5 once the CS8 parity + determinism gate went green, so an
// error here surfaces to the caller rather than silently diverging.

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// engineChangedSelection refreshes-if-needed and returns the --changed selection
// as a set of component names, computed by the engine over the full object-model
// catalog. changeOptions carries the git base/head (or an explicit --files set).
func engineChangedSelection(ctx context.Context, changeOptions git.ChangeOptions) (map[string]bool, error) {
	return selectChanged(ctx, changeOptions, planNoCatalogRefresh, os.Stderr)
}

// selectChanged is engineChangedSelection with the catalog-refresh policy and
// the notice writer made explicit (see changedCatalogView).
func selectChanged(ctx context.Context, changeOptions git.ChangeOptions, noRefresh bool, notices io.Writer) (map[string]bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	view, err := changedCatalogView(ctx, noRefresh, notices)
	if err != nil {
		return nil, err
	}
	res, err := affected.NewDetector(&view, affected.IntentImpact(intentImpact)).
		Detect(ctx, affected.GitChangeSource{Options: changeOptions, IntentPath: "intent.yaml"})
	if err != nil {
		return nil, err
	}

	// Map the engine's component keys back to the names the instance filter uses.
	keyToName := make(map[string]string, len(view.Components))
	for _, c := range view.Components {
		keyToName[c.ComponentKey] = c.Name
	}
	out := make(map[string]bool, len(res.Selection))
	for _, k := range res.Selection {
		if name := keyToName[k]; name != "" {
			out[name] = true
		}
	}
	return out, nil
}

// changedCatalogView returns the catalog --changed selects over, making sure it
// matches the working tree (OR3). The persisted snapshot at catalogs/current
// knows the components, their paths and their dependsOn edges; planning
// --changed against a snapshot built from older component manifests silently
// uses the old graph.
//
// By default it refreshes through the shared seam: the resolve memo is keyed on
// the source id plus the component-manifest digest
// (objplan.WorkspaceInputsDigest), so a fresh snapshot is a cheap memo hit and
// any manifest change re-resolves from the working tree, exactly as `orun
// catalog refresh` would. When the refresh moved catalogs/current to a
// different component graph, a one-line notice says so on out.
//
// With noRefresh (`plan --no-catalog-refresh`) the snapshot is used as-is, and
// a warning naming `orun catalog refresh` is printed when it was not built from
// the current working tree.
func changedCatalogView(ctx context.Context, noRefresh bool, out io.Writer) (objcatalog.CatalogView, error) {
	if noRefresh {
		return currentCatalogUnrefreshed(ctx, out)
	}

	// Remember what catalogs/current held before the refresh so a moved ref can
	// be compared against it. Best-effort: no prior snapshot means nothing was
	// stale, only not yet built.
	prior := ""
	if _, refs, _, err := openObjectModel(); err == nil {
		if r, rerr := refs.Read(ctx, "catalogs/current"); rerr == nil {
			prior = r.Target
		}
	}

	rc, err := refreshObjectCatalog(ctx)
	if err != nil {
		return objcatalog.CatalogView{}, err
	}
	reader := objcatalog.New(rc.store, rc.refs)
	view, err := reader.Load(ctx, "catalogs/current")
	if err != nil {
		return objcatalog.CatalogView{}, err
	}

	// Fresh snapshot: the ref did not move, so there is nothing to compare. A
	// moved ref with an identical component graph (e.g. a new commit that did
	// not touch any manifest) is not worth a notice either.
	if prior != "" && prior != string(view.ObjectID) {
		if old, lerr := reader.Load(ctx, prior); lerr == nil {
			if drift := catalogGraphDrift(old, view); drift != "" && out != nil {
				fmt.Fprintf(out, "orun: catalog was stale (%s); refreshed from the working tree\n", drift)
			}
		}
	}
	return view, nil
}

// currentCatalogUnrefreshed loads catalogs/current without refreshing it and
// warns when it was not built from the current working tree.
func currentCatalogUnrefreshed(ctx context.Context, out io.Writer) (objcatalog.CatalogView, error) {
	store, refs, root, err := openObjectModel()
	if err != nil {
		return objcatalog.CatalogView{}, err
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return objcatalog.CatalogView{}, fmt.Errorf("--changed needs a catalog snapshot and --no-catalog-refresh skipped building one: %w (run `orun catalog refresh` first)", err)
	}
	if !catalogMatchesWorkingTree(ctx, store.Algo(), root, view.ObjectID) && out != nil {
		fmt.Fprintf(out, "warning: the catalog snapshot was not built from the current working tree; --changed is using it as-is because of --no-catalog-refresh (run `orun catalog refresh` to rebuild it)\n")
	}
	return view, nil
}

// catalogMatchesWorkingTree reports whether catalogID is the catalog the
// resolve memo recorded for the current working tree: the same source id and
// the same workspace-inputs digest (CODEOWNERS, composition lock, component
// manifests). Cheap: git probes, a manifest walk and one memo read, no
// resolve. Any failure, or a missing memo entry, reports false.
func catalogMatchesWorkingTree(ctx context.Context, algo objectstore.Algo, objModelRoot string, catalogID objectstore.ObjectID) bool {
	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return false
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return false
	}
	srcID, err := nodes.SourceID(algo, objplan.BuildSourceNode(ws, sourcectx.BuildSourceSnapshotKey(ws)))
	if err != nil {
		return false
	}
	memo := objplan.NewResolveMemo(objModelRoot).WithInputsDigest(objplan.WorkspaceInputsDigest(workspaceRoot))
	// Resolver version 1 is the memo key every catalog-writing path uses
	// (objplan.Options.ResolverVersion left at its default).
	cached, ok := memo.Get(srcID, 1)
	return ok && cached == catalogID
}

// catalogGraphDrift summarises how cur's component graph differs from old's:
// components added or removed, and components whose path or dependency edges
// changed. It returns "" when the graphs are the same.
func catalogGraphDrift(old, cur objcatalog.CatalogView) string {
	oldSig := componentSignatures(old)
	curSig := componentSignatures(cur)
	added, removed, changed := 0, 0, 0
	for k, s := range curSig {
		prev, ok := oldSig[k]
		switch {
		case !ok:
			added++
		case prev != s:
			changed++
		}
	}
	for k := range oldSig {
		if _, ok := curSig[k]; !ok {
			removed++
		}
	}
	var parts []string
	if added > 0 {
		parts = append(parts, countNoun(added)+" added")
	}
	if removed > 0 {
		parts = append(parts, countNoun(removed)+" removed")
	}
	if changed > 0 {
		parts = append(parts, countNoun(changed)+" with changed paths or dependencies")
	}
	return strings.Join(parts, ", ")
}

// componentSignatures maps each component key to a string covering what
// --changed selection reads: its path, its dependsOn targets, and its typed
// relations (with the input/include/optional flags).
func componentSignatures(v objcatalog.CatalogView) map[string]string {
	out := make(map[string]string, len(v.Components))
	for _, c := range v.Components {
		deps := append([]string(nil), c.DependsOn...)
		sort.Strings(deps)
		rels := make([]string, 0, len(c.Relations))
		for _, r := range c.Relations {
			rels = append(rels, fmt.Sprintf("%s>%s:%s input=%t include=%s optional=%t", r.Type, r.ToKind, r.To, r.Input, r.Include, r.Optional))
		}
		sort.Strings(rels)
		out[c.ComponentKey] = c.Path + "\x00" + strings.Join(deps, ",") + "\x00" + strings.Join(rels, ";")
	}
	return out
}

// countNoun renders "1 component" / "N components".
func countNoun(n int) string {
	return fmt.Sprintf("%d %s", n, pluralize(n, "component", "components"))
}
