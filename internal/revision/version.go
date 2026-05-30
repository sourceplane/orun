package revision

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// marshalCanonicalJSON renders v as 2-space-indented JSON with no HTML
// escaping and a trailing newline — the canonical persistence form for every
// JSON document this package writes (data-model.md §0 / state-store.md §3).
//
// Two writes of identical struct values produce byte-identical output across
// Go versions, which the round-trip tests in writer_test.go assert via
// internal/testfx/statefs.AssertJSONFile.
//
// The encoder cannot fail for the typed values this package marshals (no
// channels, funcs, or cyclic structures), so any error here would be a
// programmer mistake. Surface it as a panic rather than smuggling a sentinel
// out through the writer signatures.
func marshalCanonicalJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(fmt.Sprintf("revision: canonical JSON encode failed: %v", err))
	}
	return buf.Bytes()
}

// stateStoreVersionPath returns the logical path for .orun/version.json
// inside a statestore. The store is rooted at .orun, so the logical path is
// simply "version.json" — the constant lives here (instead of as a helper in
// internal/statestore) to honor the M3 PR-A constraint of NOT growing the
// statestore surface for revision-specific paths.
func stateStoreVersionPath() string { return "version.json" }
