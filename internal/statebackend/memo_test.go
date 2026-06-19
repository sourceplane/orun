package statebackend

import "testing"

// The canonical golden string is the cross-language contract: it must match the
// platform's coordination-memo.test.ts byte-for-byte.
const canonicalGolden = `{"compositionLockDigest":"sha256:lock","envKeys":["A","B"],` +
	`"inputDigests":["sha256:x","sha256:y"],"steps":[{"name":"b","run":"build"}]}`

func baseInput() JobInputHashInput {
	return JobInputHashInput{
		Steps:                 []any{map[string]any{"run": "build", "name": "b"}},
		InputDigests:          []string{"sha256:y", "sha256:x"},
		EnvKeys:               []string{"B", "A"},
		CompositionLockDigest: "sha256:lock",
	}
}

func TestCanonicalizeJobInputGolden(t *testing.T) {
	if got := CanonicalizeJobInput(baseInput()); got != canonicalGolden {
		t.Fatalf("canonical form drifted from the cross-language golden:\n got: %s\nwant: %s", got, canonicalGolden)
	}
}

func TestCanonicalizeInvariantToSetOrdering(t *testing.T) {
	reordered := baseInput()
	reordered.InputDigests = []string{"sha256:x", "sha256:y"}
	reordered.EnvKeys = []string{"A", "B"}
	if CanonicalizeJobInput(reordered) != CanonicalizeJobInput(baseInput()) {
		t.Fatal("canonical form is not invariant to set-field ordering")
	}
}

func TestCanonicalizeInvariantToObjectKeyOrder(t *testing.T) {
	reordered := baseInput()
	reordered.Steps = []any{map[string]any{"name": "b", "run": "build"}}
	if CanonicalizeJobInput(reordered) != CanonicalizeJobInput(baseInput()) {
		t.Fatal("canonical form is not invariant to object key order")
	}
}

func TestCanonicalizeSensitiveToStepOrder(t *testing.T) {
	a := baseInput()
	a.Steps = []any{map[string]any{"run": "a"}, map[string]any{"run": "b"}}
	b := baseInput()
	b.Steps = []any{map[string]any{"run": "b"}, map[string]any{"run": "a"}}
	if CanonicalizeJobInput(a) == CanonicalizeJobInput(b) {
		t.Fatal("canonical form should be sensitive to step order")
	}
}

func TestJobInputHashFormatAndDeterminism(t *testing.T) {
	h := JobInputHash(baseInput())
	if len(h) != len("sha256:")+64 || h[:7] != "sha256:" {
		t.Fatalf("malformed digest: %s", h)
	}
	if h != JobInputHash(baseInput()) {
		t.Fatal("digest is not deterministic")
	}
	changed := baseInput()
	changed.InputDigests = []string{"sha256:x", "sha256:z"}
	if JobInputHash(changed) == h {
		t.Fatal("digest should change when an input digest changes")
	}
}

func TestMemoizationHit(t *testing.T) {
	result := &JobResult{JobInputHash: "sha256:abc", Outputs: []string{"sha256:o1"}, Exit: 0, LogsDigest: "sha256:log"}
	if MemoizationHit(false, result) != nil {
		t.Fatal("non-hermetic job must never memoize")
	}
	if MemoizationHit(true, nil) != nil {
		t.Fatal("hermetic miss must return nil")
	}
	if MemoizationHit(true, result) != result {
		t.Fatal("hermetic hit must return the existing result")
	}
}
