package catalogresolve

import (
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// includeAlwaysOnly normalizes an authored dependency include mode to the only
// value the catalog needs to carry: "always". The "if-selected" default (and any
// empty/unknown value) folds to "" so non-always edges add nothing to the graph
// blob — keeping existing catalog ids and goldens unchanged.
func includeAlwaysOnly(include string) string {
	if strings.EqualFold(strings.TrimSpace(include), "always") {
		return "always"
	}
	return ""
}

// resolveDependencies runs stage 8 of the resolver
// (resolution-pipeline.md §5). It rewrites every `spec.dependsOn[*]`
// reference into a fully-qualified component key when the target is
// found in the discovered set. Authored short names resolve within
// the same `(namespace, repo)` pair; fully qualified
// `<ns>/<repo>/<name>` references resolve verbatim and may cross
// repos within the workspace.
//
// Unresolved targets are left as-is in cm.Spec.Dependencies.Components
// (using the authored string as `Key`) and surface as ValidationIssue
// entries with code "component.dependency.missing". The caller decides
// the severity per the §6 table.
//
// providesApis / consumesApis / dependencies.resources.uses are
// populated from the authored values; this PR does not enforce that
// consumed APIs have a declared provider (that is C8 + --strict).
func resolveDependencies(authored []AuthoredManifest, manifests []*catalogmodel.ComponentManifest) []ValidationIssue {
	// Build an index of {componentKey, shortName} → key for fast
	// resolution. The shortName index is per-(namespace, repo) — a
	// short ref from package A in repo X resolves to package B only
	// when B lives in the same (namespace, repo).
	keyIndex := make(map[string]struct{}, len(manifests))
	shortIndex := make(map[depShortRef]string, len(manifests))
	for _, m := range manifests {
		keyIndex[m.Identity.ComponentKey] = struct{}{}
		shortIndex[depShortRef{m.Identity.Namespace, m.Identity.Repo, m.Identity.Name}] = m.Identity.ComponentKey
	}

	var issues []ValidationIssue

	for i, am := range authored {
		cm := manifests[i]
		ns := cm.Identity.Namespace
		repo := cm.Identity.Repo

		deps := make([]catalogmodel.ComponentDependency, 0, len(am.Component.Spec.DependsOn))
		for _, d := range am.Component.Spec.DependsOn {
			if d.Component == "" {
				continue
			}
			rel := d.Relationship
			if rel == "" {
				rel = catalogmodel.RelCalls
			}
			resolved, ok := resolveDepKey(d.Component, ns, repo, keyIndex, shortIndex)
			dep := catalogmodel.ComponentDependency{
				Key:          resolved,
				Name:         lastSegment(resolved),
				Relationship: rel,
				Optional:     d.Optional,
				Include:      includeAlwaysOnly(d.Include),
				Input:        d.Input,
			}
			if !ok {
				dep.Key = d.Component
				dep.Name = lastSegment(d.Component)
				issues = append(issues, ValidationIssue{
					File:     am.SourceFile,
					Pointer:  "/spec/dependsOn",
					Code:     "component.dependency.missing",
					Message:  "dependency target " + d.Component + " not found in workspace",
					Severity: SeverityWarning,
					Detail: map[string]any{
						"from": cm.Identity.ComponentKey,
						"to":   d.Component,
					},
				})
			}
			deps = append(deps, dep)
		}
		// Deterministic order: by (relationship, key).
		sort.SliceStable(deps, func(a, b int) bool {
			if deps[a].Relationship != deps[b].Relationship {
				return deps[a].Relationship < deps[b].Relationship
			}
			return deps[a].Key < deps[b].Key
		})
		cm.Spec.Dependencies.Components = deps

		// APIs (string lists) — preserve authored set, sorted.
		if am.Component.Spec.ProvidesAPIs != nil {
			out := append([]string(nil), am.Component.Spec.ProvidesAPIs...)
			sort.Strings(out)
			cm.Spec.Dependencies.APIs.Provides = out
		}
		if am.Component.Spec.ConsumesAPIs != nil {
			out := append([]string(nil), am.Component.Spec.ConsumesAPIs...)
			sort.Strings(out)
			cm.Spec.Dependencies.APIs.Consumes = out
		}
	}

	return issues
}

func resolveDepKey(authored, ns, repo string, keyIndex map[string]struct{}, shortIndex map[depShortRef]string) (string, bool) {
	if strings.Contains(authored, "/") {
		// Fully-qualified key form.
		if _, ok := keyIndex[authored]; ok {
			return authored, true
		}
		return authored, false
	}
	// Short name in same (ns, repo).
	if k, ok := shortIndex[depShortRef{ns, repo, authored}]; ok {
		return k, true
	}
	// Construct a best-effort fully-qualified form for error reporting.
	return ns + "/" + repo + "/" + authored, false
}

// depShortRef is the per-(namespace, repo) lookup key used to resolve
// short authored component references back to fully-qualified
// componentKeys.
type depShortRef struct{ ns, repo, name string }

func lastSegment(componentKey string) string {
	idx := strings.LastIndex(componentKey, "/")
	if idx < 0 {
		return componentKey
	}
	return componentKey[idx+1:]
}

// findCycles returns every simple cycle in the directed graph induced
// by the given edge type ("calls", "deploy-after", …) over the
// resolved manifests. Each returned cycle is sorted to start at the
// lexically smallest node so two equivalent cycles (a→b→a vs b→a→b)
// are de-duplicated. The edge type filter matches
// ComponentDependency.Relationship.
func findCycles(manifests []*catalogmodel.ComponentManifest, edgeType string) [][]string {
	adj := make(map[string][]string, len(manifests))
	nodes := make([]string, 0, len(manifests))
	for _, m := range manifests {
		key := m.Identity.ComponentKey
		nodes = append(nodes, key)
		for _, d := range m.Spec.Dependencies.Components {
			if d.Relationship != edgeType {
				continue
			}
			adj[key] = append(adj[key], d.Key)
		}
		sort.Strings(adj[key])
	}
	sort.Strings(nodes)

	seenCycle := map[string]struct{}{}
	var cycles [][]string

	var stack []string
	onStack := map[string]bool{}
	visited := map[string]bool{}

	var dfs func(node string)
	dfs = func(node string) {
		stack = append(stack, node)
		onStack[node] = true
		for _, next := range adj[node] {
			if onStack[next] {
				// Recover the cycle: stack from index of `next` to end.
				idx := -1
				for i, s := range stack {
					if s == next {
						idx = i
						break
					}
				}
				if idx >= 0 {
					cyc := append([]string(nil), stack[idx:]...)
					normalised := normaliseCycle(cyc)
					sig := strings.Join(normalised, "→")
					if _, dup := seenCycle[sig]; !dup {
						seenCycle[sig] = struct{}{}
						cycles = append(cycles, normalised)
					}
				}
				continue
			}
			if visited[next] {
				continue
			}
			dfs(next)
		}
		onStack[node] = false
		visited[node] = true
		stack = stack[:len(stack)-1]
	}

	for _, n := range nodes {
		if !visited[n] {
			dfs(n)
		}
	}

	sort.Slice(cycles, func(a, b int) bool {
		return strings.Join(cycles[a], "→") < strings.Join(cycles[b], "→")
	})
	return cycles
}

// normaliseCycle rotates the slice so the lexically smallest entry
// is first; that gives every equivalent cycle the same canonical form.
func normaliseCycle(cyc []string) []string {
	if len(cyc) == 0 {
		return cyc
	}
	min := 0
	for i := 1; i < len(cyc); i++ {
		if cyc[i] < cyc[min] {
			min = i
		}
	}
	out := make([]string, 0, len(cyc))
	out = append(out, cyc[min:]...)
	out = append(out, cyc[:min]...)
	return out
}
