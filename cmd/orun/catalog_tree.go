package main

// catalog_tree.go implements `orun catalog tree [component]`: render the
// catalog dependency graph (cli-surface.md §5). The text form is a left-aligned
// tree with `→` arrows and edge-type annotations; --json emits
// {nodes, edges} matching the CatalogGraph shape.
//
// --direction ∈ {out (default), in, both} controls edge traversal relative to
// the optional root component. With no root, the whole dependency graph is
// rendered as a forest rooted at the component nodes with no incoming edges
// (out direction) so the output is deterministic and acyclic-friendly.
//
// Data source: the dependencies graph of the object-model catalog
// (catalogs/current → graph/dependencies.json) via the objcatalog read view
// (loadObjCatalogView), adapted back to catalogmodel for the renderers.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var catalogTreeDirectionFlag string

// catalogGraphKindDependencies is the edge-kind key for the dependency graph
// slice in the objcatalog CatalogView.Graph map.
const catalogGraphKindDependencies = "dependencies"

// catalogTreeData is the --json payload — the CatalogGraph nodes/edges of the
// dependency graph, filtered to the reachable set when a root is given.
type catalogTreeData struct {
	Nodes []catalogmodel.GraphNode `json:"nodes"`
	Edges []catalogmodel.GraphEdge `json:"edges"`
}

func registerCatalogTreeCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "tree [component]",
		Short: "Render the catalog relationship graphs",
		Long: `Render the catalog dependency graph as a tree.

With no component argument the whole dependency graph is rendered as a forest.
Given a component the tree is rooted there and walked in the --direction:
'out' (default) follows outgoing edges, 'in' follows incoming edges, 'both'
follows either. Edges are annotated with their edge type.

Examples:
  orun catalog tree
  orun catalog tree api-edge
  orun catalog tree api-edge --direction both
  orun catalog tree --json

Exit codes:
  0  Tree rendered (possibly empty).
  1  Invalid selector or unknown --direction.
  3  StateStore failure.
  6  Catalog or graph not found.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := ""
			if len(args) == 1 {
				root = args[0]
			}
			return runCatalogTree(cmd.Context(), root)
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().StringVar(&catalogTreeDirectionFlag, "direction", "out", "Edge direction to follow: out|in|both")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogTree(ctx context.Context, root string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	root = strings.TrimSpace(root)

	dir := strings.ToLower(strings.TrimSpace(catalogTreeDirectionFlag))
	switch dir {
	case "", "out":
		dir = "out"
	case "in", "both":
	default:
		return exitErr(1, "invalid --direction %q (want out|in|both)", catalogTreeDirectionFlag)
	}

	view, _, err := loadObjCatalog(ctx)
	if err != nil {
		return err
	}

	graph := objGraphToCatalogModel(view.Graph[catalogGraphKindDependencies])

	nodes, edges := filterGraph(graph, root, dir)

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogTreeResult, catalogTreeData{
			Nodes: nodes,
			Edges: edges,
		}, nil)
	}
	return renderCatalogTreeText(graph, root, dir)
}

// filterGraph returns the reachable node/edge subset for the --json payload.
// With no root the full graph is returned (sorted). With a root the BFS over
// the chosen direction collects the reachable nodes and the traversed edges.
func filterGraph(g catalogmodel.CatalogGraph, root, dir string) ([]catalogmodel.GraphNode, []catalogmodel.GraphEdge) {
	if root == "" {
		nodes := append([]catalogmodel.GraphNode(nil), g.Nodes...)
		edges := append([]catalogmodel.GraphEdge(nil), g.Edges...)
		sortGraph(nodes, edges)
		return nodes, edges
	}

	rootKey := resolveGraphNodeKey(g, root)
	reachableNodes := map[string]bool{}
	var keptEdges []catalogmodel.GraphEdge
	if rootKey != "" {
		reachableNodes[rootKey] = true
		queue := []string{rootKey}
		seenEdge := map[string]bool{}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, e := range g.Edges {
				var next string
				switch dir {
				case "out":
					if e.From == cur {
						next = e.To
					}
				case "in":
					if e.To == cur {
						next = e.From
					}
				case "both":
					if e.From == cur {
						next = e.To
					} else if e.To == cur {
						next = e.From
					}
				}
				if next == "" {
					continue
				}
				ek := e.From + "\x00" + e.To + "\x00" + e.Type
				if !seenEdge[ek] {
					seenEdge[ek] = true
					keptEdges = append(keptEdges, e)
				}
				if !reachableNodes[next] {
					reachableNodes[next] = true
					queue = append(queue, next)
				}
			}
		}
	}

	var nodes []catalogmodel.GraphNode
	for _, n := range g.Nodes {
		if reachableNodes[n.Key] {
			nodes = append(nodes, n)
		}
	}
	sortGraph(nodes, keptEdges)
	return nodes, keptEdges
}

// resolveGraphNodeKey maps a bare name or key to a node key present in the
// graph. Exact key match wins; otherwise the first node whose Name matches.
func resolveGraphNodeKey(g catalogmodel.CatalogGraph, root string) string {
	for _, n := range g.Nodes {
		if n.Key == root {
			return n.Key
		}
	}
	for _, n := range g.Nodes {
		if n.Name == root {
			return n.Key
		}
	}
	return ""
}

func sortGraph(nodes []catalogmodel.GraphNode, edges []catalogmodel.GraphEdge) {
	sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].Key < nodes[b].Key })
	sort.SliceStable(edges, func(a, b int) bool {
		if edges[a].From != edges[b].From {
			return edges[a].From < edges[b].From
		}
		if edges[a].To != edges[b].To {
			return edges[a].To < edges[b].To
		}
		return edges[a].Type < edges[b].Type
	})
}

func renderCatalogTreeText(g catalogmodel.CatalogGraph, root, dir string) error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	out := os.Stdout

	nameByKey := map[string]string{}
	for _, n := range g.Nodes {
		nameByKey[n.Key] = n.Name
	}

	// Adjacency in the requested direction.
	adj := map[string][]catalogmodel.GraphEdge{}
	for _, e := range g.Edges {
		switch dir {
		case "out":
			adj[e.From] = append(adj[e.From], e)
		case "in":
			adj[e.To] = append(adj[e.To], e)
		case "both":
			adj[e.From] = append(adj[e.From], e)
			adj[e.To] = append(adj[e.To], e)
		}
	}

	var roots []string
	if root != "" {
		k := resolveGraphNodeKey(g, root)
		if k == "" {
			fmt.Fprintf(out, "Component %q not found in dependency graph.\n", root)
			return nil
		}
		roots = []string{k}
	} else {
		roots = forestRoots(g, dir)
	}

	if len(g.Nodes) == 0 {
		fmt.Fprintln(out, "Dependency graph is empty.")
		return nil
	}

	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Dependency tree"))
	for _, rk := range roots {
		printTreeNode(out, rk, nameByKey, adj, dir, "", map[string]bool{})
	}
	return nil
}

// forestRoots returns the node keys with no incoming edge in the chosen
// direction so the whole graph renders as a deterministic forest. Falls back
// to every node (sorted) when each node has a parent (a pure cycle).
func forestRoots(g catalogmodel.CatalogGraph, dir string) []string {
	hasParent := map[string]bool{}
	for _, e := range g.Edges {
		switch dir {
		case "out":
			hasParent[e.To] = true
		case "in":
			hasParent[e.From] = true
		case "both":
			hasParent[e.To] = true
			hasParent[e.From] = true
		}
	}
	var roots []string
	for _, n := range g.Nodes {
		if !hasParent[n.Key] {
			roots = append(roots, n.Key)
		}
	}
	if len(roots) == 0 {
		for _, n := range g.Nodes {
			roots = append(roots, n.Key)
		}
	}
	sort.Strings(roots)
	return roots
}

func printTreeNode(out *os.File, key string, nameByKey map[string]string, adj map[string][]catalogmodel.GraphEdge, dir, indent string, onPath map[string]bool) {
	name := nameByKey[key]
	if name == "" {
		name = key
	}
	if onPath[key] {
		fmt.Fprintf(out, "%s%s (cycle)\n", indent, name)
		return
	}
	fmt.Fprintf(out, "%s%s\n", indent, name)

	onPath[key] = true
	defer delete(onPath, key)

	edges := append([]catalogmodel.GraphEdge(nil), adj[key]...)
	sort.SliceStable(edges, func(a, b int) bool {
		na, nb := neighborOf(edges[a], key, dir), neighborOf(edges[b], key, dir)
		if na != nb {
			return na < nb
		}
		return edges[a].Type < edges[b].Type
	})
	for _, e := range edges {
		nb := neighborOf(e, key, dir)
		if nb == "" {
			continue
		}
		nbName := nameByKey[nb]
		if nbName == "" {
			nbName = nb
		}
		opt := ""
		if e.Optional {
			opt = " (optional)"
		}
		fmt.Fprintf(out, "%s  → [%s]%s\n", indent, e.Type, opt)
		printTreeNode(out, nb, nameByKey, adj, dir, indent+"    ", onPath)
	}
}

// neighborOf returns the node on the far side of edge e relative to `key` in
// the chosen direction, or "" when e does not touch key in that direction.
func neighborOf(e catalogmodel.GraphEdge, key, dir string) string {
	switch dir {
	case "out":
		if e.From == key {
			return e.To
		}
	case "in":
		if e.To == key {
			return e.From
		}
	case "both":
		if e.From == key {
			return e.To
		}
		if e.To == key {
			return e.From
		}
	}
	return ""
}
