package services

// catalog_source.go is the cockpit's read-side freshness gate (design.md §3.1):
// it sources the component list from the object-model catalog at
// catalogs/current when that catalog was resolved against the workspace's
// current source (a clean, unchanged tree), and otherwise reports a miss so the
// caller falls back to the live intent loader. The CatalogComponentView →
// ComponentSummary mapping lives here, in services (consumers.md §3.2).

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/cockpit/catalogread"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// freshCatalogComponents reads the component list from the object-model catalog
// when it is fresh for workspaceRoot. It returns ok=false — so the caller uses
// the live intent loader — when the object model is absent/empty/unreadable, the
// stored catalog was resolved against a different (e.g. dirty) tree, or the
// catalog has no components. The cockpit does not resolve+write-through on a
// miss; the §0 universal refresh hook and explicit orun commands keep the store
// fresh (a cockpit-side resolve is a tracked follow-up).
func (s *LiveOrunService) freshCatalogComponents(ctx context.Context, workspaceRoot string) ([]ComponentSummary, bool) {
	if s.cfg.ObjectModelRoot == "" {
		return nil, false
	}
	root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	// Only adopt the object model if it actually has content (mirrors objReader).
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, false
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, false
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui"})
	if err != nil {
		return nil, false
	}

	cat, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return nil, false
	}
	if !catalogFresh(ctx, store, cat.SourceID, workspaceRoot) {
		return nil, false
	}
	if len(cat.Components) == 0 {
		return nil, false
	}
	return catalogComponentSummaries(cat.Components), true
}

// catalogChangeOverlay computes the Q2 changed/affected overlay (consumers.md
// §2, environments.md §3) by running the change-detection engine over the
// object-model catalog against the current working tree, returning a
// component-name → kind map ("changed" for a directly-changed component,
// "affected" for one impacted via a dependency). It composes the read seam
// (internal/cockpit/catalogread → internal/affected), so the cockpit never
// touches the engine directly.
//
// Unlike freshCatalogComponents, the overlay is computed regardless of tree
// cleanliness: a clean tree simply yields an empty map (nothing changed), while
// a dirty tree surfaces the in-flight edits the catalog was last resolved
// against. Best-effort: an absent/unreadable store or a detection error returns
// a nil map so the component list renders with no badges rather than failing.
func (s *LiveOrunService) catalogChangeOverlay(ctx context.Context, workspaceRoot string) map[string]string {
	if s.cfg.ObjectModelRoot == "" {
		return nil
	}
	root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui"})
	if err != nil {
		return nil
	}

	view, err := catalogread.New(store, refs, workspaceRoot).CatalogView(ctx, true)
	if err != nil || !view.Overlay {
		return nil
	}
	out := make(map[string]string)
	for _, row := range view.Components {
		switch {
		case row.DirectlyChanged:
			out[row.Name] = "changed"
		case row.Dependent:
			out[row.Name] = "affected"
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyChangeOverlay annotates a component list with an overlay map (by name),
// setting Changed + ChangeKind. A nil overlay leaves the list untouched.
func applyChangeOverlay(comps []ComponentSummary, overlay map[string]string) {
	if overlay == nil {
		return
	}
	for i := range comps {
		if kind, ok := overlay[comps[i].Name]; ok {
			comps[i].Changed = true
			comps[i].ChangeKind = kind
		}
	}
}

// catalogFresh reports whether catalogSourceID equals the content id of the
// current workspace source — i.e. the stored catalog was resolved against the
// tree as it is now (the §3.1 gate). Any resolution/hashing error fails closed
// (not fresh → fall back to the live loader), so a stale read never wins.
func catalogFresh(ctx context.Context, store *objectstore.LocalStore, catalogSourceID, workspaceRoot string) bool {
	if catalogSourceID == "" {
		return false
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return false
	}
	src := objplan.BuildSourceNode(ws, sourcectx.BuildSourceSnapshotKey(ws))
	id, err := nodes.SourceID(store.Algo(), src)
	if err != nil {
		return false
	}
	return string(id) == catalogSourceID
}

// catalogComponentSummaries maps the object-model catalog's components into the
// cockpit's ComponentSummary. Envs are the sorted environment-binding names;
// Profile is the first non-empty binding profile in name order.
func catalogComponentSummaries(comps []objcatalog.CatalogComponentView) []ComponentSummary {
	out := make([]ComponentSummary, 0, len(comps))
	for _, c := range comps {
		out = append(out, ComponentSummary{
			Name:      c.Name,
			Type:      c.Type,
			Domain:    c.Domain,
			Path:      c.Path,
			Envs:      sortedEnvNames(c.Environments),
			Profile:   firstProfile(c.Environments),
			DependsOn: append([]string(nil), c.DependsOn...),
			Watches:   specWatches(c.Spec),
		})
	}
	return out
}

func sortedEnvNames(envs map[string]objcatalog.EnvView) []string {
	if len(envs) == 0 {
		return nil
	}
	out := make([]string, 0, len(envs))
	for name := range envs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func firstProfile(envs map[string]objcatalog.EnvView) string {
	for _, name := range sortedEnvNames(envs) {
		if p := envs[name].Profile; p != "" {
			return p
		}
	}
	return ""
}

// specWatches reads spec.change.watches from the verbatim manifest spec.
func specWatches(spec map[string]any) []string {
	change, ok := spec["change"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := change["watches"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, w := range raw {
		if s, ok := w.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
