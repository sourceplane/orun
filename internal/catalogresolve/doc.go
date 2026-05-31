// Package catalogresolve implements the first three stages of the
// component-catalog resolution pipeline (resolution-pipeline.md §1):
//
//  1. discover  — walk the workspace for component.yaml / .yml files,
//     honouring intent.yaml `catalog.discovery.exclude`.
//  2. load      — YAML-decode each authored manifest and JSON-Schema
//     validate it against the embedded
//     internal/catalogmodel.ComponentYAMLSchema artifact.
//  3. inherit   — fill missing authored fields from intent.yaml
//     `catalog.defaults`, per the explicit-vs-unset rules in
//     resolution-pipeline.md §3.
//
// The output is a deterministic in-memory []AuthoredManifest produced by
// DiscoverAndLoad. This package does NOT yet implement composition
// defaults (stage 4), inference (stage 6), dependency resolution (stage
// 8), validation matrix (stage 9), manifestHash (stage 10), or any of
// the graph/snapshot stages — those land in the C2 second PR and C3.
//
// # Boundary
//
// catalogresolve depends only on:
//   - the Go stdlib;
//   - gopkg.in/yaml.v3 (already a workspace dep);
//   - github.com/santhosh-tekuri/jsonschema/v5 (already a workspace dep);
//   - internal/catalogmodel (pure data + canonical encoder + embedded schema).
//
// It MUST NOT import internal/sourcectx, internal/catalogstore, or any
// Phase 1 package. The output of DiscoverAndLoad is the *input* to a
// future Resolve(ctx, opts) that combines it with a sourcectx.WorkspaceState.
//
// # Determinism
//
// Two consecutive calls to DiscoverAndLoad on the same fixture produce
// byte-identical output. Walks use filepath.WalkDir + lexical sort; lists
// are sorted before return; all map iteration goes through sorted keys.
package catalogresolve
