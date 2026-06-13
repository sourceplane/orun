package work

import _ "embed"

// Schema is the canonical, generated JSON Schema for the work-plane persisted
// shapes (data-model.md). It is produced by `go generate ./internal/work/...`
// from the Go types in this package and verified clean on every push by the
// `make verify-generated` gate.
//
// Consumers MUST treat the slice as read-only; the backend Worker validates the
// same shapes against this artifact so the two implementations cannot drift.
//
//go:embed schema/work.schema.json
var Schema []byte
