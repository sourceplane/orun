package main

// catalog_diff.go implements `orun catalog diff [component]`: compare two
// resolved catalog snapshots (a base and a head) and report the
// component-level and graph-level differences (cli-surface.md §6).
//
// The base/head selectors reuse the shared object-model catalog ref grammar
// (current|main|latest|branches/<name>|prs/<n>|<catalog-id>) resolved by
// objCatalogRef — the same grammar `--source` accepts. Defaults: --base main,
// --head current (the common "what changed on my branch vs main" question).
//
// The command is glue only: it loads each side as an object-model catalog view,
// reconstructs a catalogdiff.Snapshot (typed manifests + the relationship
// graphs) from it, and hands the pair to the pure internal/catalogdiff engine.
// All comparison logic — set-vs-list field rules, graph node/edge diffing,
// deterministic ordering — lives there.
//
// Exit 0 even when differences exist (a diff is a successful comparison, not a
// failure). Non-zero only for real errors: exit 1 invalid selector, exit 3
// store failure, exit 6 a catalog/component that does not resolve.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/catalogdiff"
	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	catalogDiffBaseFlag string
	catalogDiffHeadFlag string
)

// catalogDiffEndpoint identifies one resolved side of the diff for the JSON
// payload: the selector the user passed plus the keys it resolved to.
type catalogDiffEndpoint struct {
	Selector           string `json:"selector"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
}

// catalogDiffData is the CatalogDiffResult `data` payload: the two resolved
// endpoints plus the engine Result fields. Field names are the stable §6/§11
// JSON contract.
type catalogDiffData struct {
	Base         catalogDiffEndpoint           `json:"base"`
	Head         catalogDiffEndpoint           `json:"head"`
	Component    string                        `json:"component,omitempty"`
	Changed      []catalogdiff.ComponentChange `json:"changed"`
	Added        []catalogdiff.ComponentRef    `json:"added"`
	Removed      []catalogdiff.ComponentRef    `json:"removed"`
	GraphChanges []catalogdiff.GraphChange     `json:"graphChanges"`
}

func registerCatalogDiffCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "diff [component]",
		Short: "Compare two catalog snapshots",
		Long: `Compare two resolved catalog snapshots and report what changed.

Resolves a base and a head snapshot via the shared ref selector grammar and
reports the differences: changed components (with field path, base value, and
head value), added and removed components, and graph changes. Set-shaped
fields (tags, providesApis, consumesApis) are compared order-insensitively;
dependsOn is order-sensitive. A bare component argument narrows the report to
that component.

Defaults are --base main --head current.

Examples:
  orun catalog diff --base main --head current
  orun catalog diff --base main --head pr-139
  orun catalog diff api-edge --base main --head current
  orun catalog diff --json

Exit codes:
  0  Comparison succeeded (differences, if any, are reported — not an error).
  1  Invalid base/head selector.
  3  StateStore failure.
  6  A base/head catalog or the named component did not resolve.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			component := ""
			if len(args) == 1 {
				component = args[0]
			}
			return runCatalogDiff(cmd.Context(), component)
		},
	}

	cmd.Flags().StringVar(&catalogDiffBaseFlag, "base", "", "Base snapshot selector (default main)")
	cmd.Flags().StringVar(&catalogDiffHeadFlag, "head", "", "Head snapshot selector (default current)")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogDiff(ctx context.Context, component string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	component = strings.TrimSpace(component)

	// Defaults: base main, head current — the common branch-vs-main question.
	baseStr := strings.TrimSpace(catalogDiffBaseFlag)
	if baseStr == "" {
		baseStr = catalogmodel.RefNameMain
	}
	headStr := strings.TrimSpace(catalogDiffHeadFlag)
	if headStr == "" {
		headStr = catalogmodel.RefNameCurrent
	}

	// Resolve both selectors to object-model catalog refs first, so a malformed
	// --base/--head fails fast with exit 1 before any store work.
	baseRef, err := objCatalogRef(baseStr, "")
	if err != nil {
		return err
	}
	headRef, err := objCatalogRef(headStr, "")
	if err != nil {
		return err
	}

	store, refs, _, err := openObjectModel()
	if err != nil {
		return exitErr(3, "open object model: %w", err)
	}
	reader := objcatalog.New(store, refs)

	baseSnap, baseView, err := loadObjDiffSnapshot(ctx, reader, baseStr, baseRef)
	if err != nil {
		return err
	}
	headSnap, headView, err := loadObjDiffSnapshot(ctx, reader, headStr, headRef)
	if err != nil {
		return err
	}

	result := catalogdiff.Diff(baseSnap, headSnap)
	if component != "" {
		if !diffHasComponent(baseSnap, headSnap, component) {
			return exitErr(6, "component %q not found in base or head catalog", component)
		}
		result = result.FilterComponent(component)
	}

	data := catalogDiffData{
		Base:         objEndpointFor(baseStr, baseView),
		Head:         objEndpointFor(headStr, headView),
		Component:    component,
		Changed:      result.Changed,
		Added:        result.Added,
		Removed:      result.Removed,
		GraphChanges: result.GraphChanges,
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogDiffResult, data, nil)
	}
	return renderCatalogDiffText(data)
}

// loadObjDiffSnapshot loads one object-model catalog view by ref and assembles
// the catalogdiff.Snapshot: every component reconstructed as a typed manifest
// (buildObjManifest) plus the relationship graphs the view carries. The diff
// engine compares only Spec/Metadata/graph fields — all faithfully recovered —
// so the object-model Source/Runtime gaps never surface as spurious changes.
func loadObjDiffSnapshot(ctx context.Context, reader *objcatalog.Reader, selector, ref string) (catalogdiff.Snapshot, objcatalog.CatalogView, error) {
	view, err := reader.Load(ctx, ref)
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return catalogdiff.Snapshot{}, objcatalog.CatalogView{},
				exitErr(6, "resolve catalog %q: not found (run 'orun catalog refresh' first): %w", selector, err)
		}
		return catalogdiff.Snapshot{}, objcatalog.CatalogView{}, exitErr(3, "resolve catalog %q: %w", selector, err)
	}
	snap := catalogdiff.Snapshot{
		Components: make([]catalogmodel.ComponentManifest, 0, len(view.Components)),
		Graphs:     make(map[string]catalogmodel.CatalogGraph, len(view.Graph)),
	}
	for _, c := range view.Components {
		snap.Components = append(snap.Components, buildObjManifest(view, c))
	}
	for kind, gv := range view.Graph {
		snap.Graphs[kind] = objGraphToCatalogModel(gv)
	}
	return snap, view, nil
}

// diffHasComponent reports whether the named component (bare name or full key)
// exists in either snapshot — so a `diff <component>` for an unknown component
// fails with exit 6 rather than silently rendering an empty report.
func diffHasComponent(base, head catalogdiff.Snapshot, name string) bool {
	for _, snap := range []catalogdiff.Snapshot{base, head} {
		for _, m := range snap.Components {
			if m.Identity.Name == name || m.Identity.ComponentKey == name {
				return true
			}
		}
	}
	return false
}

// objEndpointFor builds the JSON endpoint record from the selector string and
// the resolved object-model catalog view.
func objEndpointFor(selector string, view objcatalog.CatalogView) catalogDiffEndpoint {
	return catalogDiffEndpoint{
		Selector:           selector,
		SourceSnapshotKey:  view.SourceID,
		CatalogSnapshotKey: objCatalogSnapshotKey(view),
	}
}

func renderCatalogDiffText(d catalogDiffData) error {
	out := os.Stdout
	color := ui.ColorEnabledForWriter(out)

	header := fmt.Sprintf("Catalog diff: %s → %s", d.Base.Selector, d.Head.Selector)
	fmt.Fprintf(out, "%s\n", ui.Bold(color, header))
	fmt.Fprintf(out, "  base: %s\n", dashKey(d.Base.CatalogSnapshotKey))
	fmt.Fprintf(out, "  head: %s\n", dashKey(d.Head.CatalogSnapshotKey))
	if d.Component != "" {
		fmt.Fprintf(out, "  component: %s\n", d.Component)
	}
	fmt.Fprintln(out)

	if len(d.Changed) == 0 && len(d.Added) == 0 && len(d.Removed) == 0 && len(d.GraphChanges) == 0 {
		fmt.Fprintln(out, "No differences.")
		return nil
	}

	// Section order is load-bearing (§6): Changed, Added, Removed, Graph.
	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Changed components"))
	if len(d.Changed) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, c := range d.Changed {
			fmt.Fprintf(out, "  %s\n", c.Name)
			for _, f := range c.Fields {
				fmt.Fprintf(out, "    %s: %s → %s\n", f.Path, diffVal(f.Base), diffVal(f.Head))
			}
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Added components"))
	if len(d.Added) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, a := range d.Added {
			fmt.Fprintf(out, "  + %s (%s)\n", a.Name, a.ComponentKey)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Removed components"))
	if len(d.Removed) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, r := range d.Removed {
			fmt.Fprintf(out, "  - %s (%s)\n", r.Name, r.ComponentKey)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Graph changes"))
	if len(d.GraphChanges) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, g := range d.GraphChanges {
			fmt.Fprintf(out, "  %s\n", renderGraphChange(g))
		}
	}
	return nil
}

// renderGraphChange formats one graph change for the text output.
func renderGraphChange(g catalogdiff.GraphChange) string {
	switch g.Change {
	case "node-added":
		return fmt.Sprintf("[%s] + node %s", g.Graph, nodeLabel(g))
	case "node-removed":
		return fmt.Sprintf("[%s] - node %s", g.Graph, nodeLabel(g))
	case "edge-added":
		return fmt.Sprintf("[%s] + edge %s →[%s] %s", g.Graph, g.From, g.Type, g.To)
	case "edge-removed":
		return fmt.Sprintf("[%s] - edge %s →[%s] %s", g.Graph, g.From, g.Type, g.To)
	default:
		return fmt.Sprintf("[%s] %s", g.Graph, g.Change)
	}
}

func nodeLabel(g catalogdiff.GraphChange) string {
	if g.Name != "" {
		return g.Name + " (" + g.Key + ")"
	}
	return g.Key
}

// diffVal renders a possibly-empty field value for text output: an empty value
// shows as "∅" so a clear→set or set→clear transition is legible.
func diffVal(v string) string {
	if v == "" {
		return "∅"
	}
	return v
}

func dashKey(k string) string {
	if k == "" {
		return "-"
	}
	return k
}
