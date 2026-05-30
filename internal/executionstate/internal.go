package executionstate

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/statestore"
)

// marshalCanonicalJSON renders v as 2-space-indented JSON with no HTML
// escaping and a trailing newline — the canonical persistence form for
// every JSON document this package writes (data-model.md §0 /
// state-store.md §3). Mirrors the encoder in
// internal/revision/version.go::marshalCanonicalJSON; we duplicate the
// helper rather than lift a shared utility because the M4 plan
// (implementation-plan.md §M4) calls out keeping the packages
// independent, and the revision-side helper is unexported.
//
// Two writes of identical struct values produce byte-identical output
// across Go versions; round-trip tests in model_test.go assert this via
// internal/testfx/statefs.AssertJSONFile.
//
// The encoder cannot fail for the typed values this package marshals
// (no channels, funcs, or cyclic structures), so any error here would
// be a programmer mistake. Surface it as a panic rather than smuggling a
// sentinel out through the writer signatures.
func marshalCanonicalJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(fmt.Sprintf("executionstate: canonical JSON encode failed: %v", err))
	}
	return buf.Bytes()
}

// strictJSON unmarshals raw into out with DisallowUnknownFields. Mirrors
// internal/revision.strictJSON — the helper cannot live in a shared
// package without growing the M4 dependency surface beyond what
// implementation-plan.md §5.1 sanctions.
func strictJSON(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", statestore.ErrInvalid, err)
	}
	return nil
}

// equalBytes is a thin alias for bytes.Equal, named for readability at
// the CAS short-circuit call site in MarkTerminal.
func equalBytes(a, b []byte) bool { return bytes.Equal(a, b) }

// looseUnmarshal is json.Unmarshal wrapped to map decode failures onto
// statestore.ErrInvalid for callers that need a sentinel chain.
func looseUnmarshal(raw []byte, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %v", statestore.ErrInvalid, err)
	}
	return nil
}
