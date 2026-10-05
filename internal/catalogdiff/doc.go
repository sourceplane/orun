// Package catalogdiff compares two resolved catalog snapshots and reports the
// component-level and graph-level differences between them.
//
// # Authority
//
// Behavioral spec source: specs/archive/orun-component-catalog/cli-surface.md §6
// (`orun catalog diff`). The diff is the engine behind that command; the CLI
// layer (cmd/orun/catalog_diff.go) is glue that resolves the base/head
// snapshots, hands them to Diff, and renders the Result.
//
// # Determinism contract
//
// Diff output is deterministic across runs for the same inputs: every output
// slice (changed/added/removed components, field changes, graph changes) is
// sorted by a total key order before return. Set-shaped component fields
// (`tags`, `providesApis`, `consumesApis`) are compared order-insensitively;
// `dependsOn` is compared order-sensitively per the §6 contract.
//
// # Boundary
//
// catalogdiff is a pure comparison package. It imports only catalogmodel (the
// persisted data types) and the standard library — never the CLI, the store,
// or the resolver. Callers assemble a Snapshot (the resolved manifests plus
// the relationship graphs) from whatever source they like and pass it in.
package catalogdiff
