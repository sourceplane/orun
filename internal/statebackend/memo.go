package statebackend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Coordination memoization (BM1/NC1 — coordination-api.md §1, §8.4). The Go side
// of the platform's @saas/contracts/coordination memo helpers. CanonicalizeJobInput
// reproduces the platform's canonical form byte-for-byte (pinned by the shared
// golden string in memo_test.go); the digest is its sha256.

// JobResult is a completed job's content-addressed result object (`job-result`).
type JobResult struct {
	JobInputHash string   `json:"jobInputHash"`
	Outputs      []string `json:"outputs"`
	Exit         int      `json:"exit"`
	LogsDigest   string   `json:"logsDigest"`
}

// JobInputHashInput is the hermetic input set a jobInputHash is computed over
// (C5): resolved step definitions, declared input object digests, declared
// environment-variable KEYS (never values), and the composition-lock digest. It
// excludes wall-clock, secret values, and runner identity.
type JobInputHashInput struct {
	Steps                 any
	InputDigests          []string
	EnvKeys               []string
	CompositionLockDigest string
}

// CanonicalizeJobInput returns the normative canonical serialization of a job's
// inputs — identical to the platform's canonicalizeJobInput(). Object keys are
// sorted recursively; set-like fields are sorted; step order is preserved.
func CanonicalizeJobInput(input JobInputHashInput) string {
	inputDigests := append([]string(nil), input.InputDigests...)
	envKeys := append([]string(nil), input.EnvKeys...)
	sort.Strings(inputDigests)
	sort.Strings(envKeys)
	obj := map[string]any{
		"steps":                 input.Steps,
		"inputDigests":          toAnySlice(inputDigests),
		"envKeys":               toAnySlice(envKeys),
		"compositionLockDigest": input.CompositionLockDigest,
	}
	return canonicalJSON(obj)
}

// JobInputHash is "sha256:<hex>" of the canonical job inputs.
func JobInputHash(input JobInputHashInput) string {
	sum := sha256.Sum256([]byte(CanonicalizeJobInput(input)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// MemoizationHit is the opt-in hermetic gate (C6/D1): only a hermetic job may
// reuse a prior result; a non-hermetic job is never memoized (default off).
func MemoizationHit(hermetic bool, existing *JobResult) *JobResult {
	if !hermetic {
		return nil
	}
	return existing
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// canonicalJSON serializes a JSON-like value deterministically, matching
// JavaScript's JSON.stringify with recursively sorted object keys (no HTML
// escaping, so strings match JS for <, >, &).
func canonicalJSON(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return quoteJSString(t)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = canonicalJSON(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = quoteJSString(k) + ":" + canonicalJSON(t[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		// Numbers and any other JSON scalar: marshal without HTML escaping.
		return marshalNoEscape(v)
	}
}

func quoteJSString(s string) string {
	return marshalNoEscape(s)
}

func marshalNoEscape(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}
