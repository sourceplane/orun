package worklens

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Sealing — the system of proof (data-model.md §8). A SpecSnapshot is the
// frozen brief an agent pulls: INTENT PLANE ONLY by type — it structurally
// cannot carry a rung, an assignee, or any other fold output (invariant 1).
// Both logs seal as chained segments whose prev links make the audit record
// tamper-evident. Identity is content: canonical JSON, sha256 (invariant 7).

// SpecSnapshot is the frozen epic: spec envelope + task envelopes with
// contracts, pinned to the two log cursors it reflects. No lifecycle field
// exists on any of these types.
type SpecSnapshot struct {
	Kind       string `json:"kind"` // "SpecSnapshot"
	APIVersion string `json:"apiVersion"`
	Spec       Spec   `json:"spec"`
	Tasks      []Task `json:"tasks"`
	Catalog    string `json:"catalog,omitempty"` // CatalogSnapshot id the affects keys resolved against
	CoordSeq   int64  `json:"coordSeq"`          // coordination-log cursor at seal time
	ObsSeq     int64  `json:"obsSeq"`            // observation-log cursor at seal time
}

// CoordinationSegment seals a coordination-log range; Prev chains segments.
type CoordinationSegment struct {
	Kind       string              `json:"kind"` // "WorkCoordinationSegment"
	APIVersion string              `json:"apiVersion"`
	Workspace  string              `json:"workspace"`
	FromSeq    int64               `json:"fromSeq"`
	ToSeq      int64               `json:"toSeq"`
	Events     []CoordinationEvent `json:"events"`
	Prev       string              `json:"prev,omitempty"`
}

// ObservationSegment seals an observation-log range; Prev chains segments.
type ObservationSegment struct {
	Kind         string        `json:"kind"` // "WorkObservationSegment"
	APIVersion   string        `json:"apiVersion"`
	Workspace    string        `json:"workspace"`
	FromSeq      int64         `json:"fromSeq"`
	ToSeq        int64         `json:"toSeq"`
	Observations []Observation `json:"observations"`
	Prev         string        `json:"prev,omitempty"`
}

// NewSpecSnapshot freezes a spec and its tasks at the given log cursors.
// Tasks are ordered by key so identical inputs seal byte-identically
// regardless of input order.
func NewSpecSnapshot(spec Spec, tasks []Task, catalog string, coordSeq, obsSeq int64) SpecSnapshot {
	ordered := append([]Task(nil), tasks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })
	return SpecSnapshot{
		Kind:       "SpecSnapshot",
		APIVersion: APIVersion,
		Spec:       spec,
		Tasks:      ordered,
		Catalog:    catalog,
		CoordSeq:   coordSeq,
		ObsSeq:     obsSeq,
	}
}

// CanonicalJSON encodes v deterministically: lexicographically sorted keys,
// no insignificant whitespace, UTF-8 — the same logical content yields the
// same bytes on every machine (invariant 7).
func CanonicalJSON(v interface{}) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("worklens: canonical encode: %w", err)
	}
	var tree interface{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("worklens: canonical decode: %w", err)
	}
	out := make([]byte, 0, len(raw))
	return appendCanonical(out, tree)
}

func appendCanonical(out []byte, v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, '{')
		for i, k := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			out = append(out, kb...)
			out = append(out, ':')
			out, err = appendCanonical(out, t[k])
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	case []interface{}:
		out = append(out, '[')
		for i, e := range t {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendCanonical(out, e)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		return append(out, b...), nil
	}
}

// ContentID returns "sha256:<hex>" over the canonical bytes of v — the same
// object has the same id on every machine.
func ContentID(v interface{}) (string, []byte, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), b, nil
}

// forbidden hot-state keys: sealing refuses any payload that smuggles fold
// output into the proof plane (the type system prevents it for our own
// shapes; this guards future field additions and imported docs).
var hotStateKeys = []string{"\"rung\"", "\"lifecycle\"", "\"assignees\"", "\"pinned\""}

// SealSpecSnapshot canonicalizes, verifies the no-hot-state invariant, and
// returns (contentID, canonicalBytes).
func SealSpecSnapshot(s SpecSnapshot) (string, []byte, error) {
	id, b, err := ContentID(s)
	if err != nil {
		return "", nil, err
	}
	for _, k := range hotStateKeys {
		if containsToken(b, k) {
			return "", nil, fmt.Errorf("worklens: snapshot carries hot state (%s) — invariant 1", k)
		}
	}
	return id, b, nil
}

func containsToken(b []byte, token string) bool {
	if len(token) == 0 || len(b) < len(token) {
		return false
	}
	for i := 0; i+len(token) <= len(b); i++ {
		if string(b[i:i+len(token)]) == token {
			return true
		}
	}
	return false
}
