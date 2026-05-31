package catalogmodel

import _ "embed"

// ComponentYAMLSchema is the canonical, generated JSON Schema artifact for the
// authored `component.yaml` shape (data-model.md §6).
//
// It is the single source of truth for schema-validation in downstream
// resolver packages (`internal/catalogresolve` and beyond). The bytes are
// produced by `go generate ./internal/catalogmodel/...` from
// `ComponentYAML`'s reflected Go type and verified clean on every push by
// the `make verify-generated` gate (Makefile §verify-generated).
//
// Consumers MUST treat the slice as read-only. Do not vendor a copy in
// other packages — re-export by reference and let the embed pin determinism.
//
//go:embed schema/component-yaml.schema.json
var ComponentYAMLSchema []byte
