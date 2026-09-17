package remotestate

import (
	"encoding/json"
	"strings"
	"testing"
)

// THE DETAIL IS THE WHOLE ANSWER. A 422 from the platform names the field it
// rejected; without it every validation failure reads identically and says
// nothing about what to change. This cost a live debugging round: a bootstrap
// died on `Validation failed (code: validation_failed) [requestId: …]` when the
// platform had plainly said `epic: no epic with that ref, key or slug`.
func TestAPIErrorNamesTheFieldThePlatformRejected(t *testing.T) {
	err := &APIError{
		Message:   "Validation failed",
		Code:      "validation_failed",
		RequestID: "req_c104e8190f8fe60b058f1d82",
		Details:   json.RawMessage(`{"fields":{"epic":["no epic with that ref, key or slug"]}}`),
	}
	got := err.Error()
	if !strings.Contains(got, "epic: no epic with that ref, key or slug") {
		t.Fatalf("error = %q, want the rejected field and its reason", got)
	}
	// The parts that were already there must survive.
	for _, want := range []string{"Validation failed", "code: validation_failed", "req_c104e8190f8fe60b058f1d82"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error = %q, lost %q", got, want)
		}
	}
}

// Several rejected fields read in a stable order, so the same failure produces
// the same message — in a log, in a test, in a bug report.
func TestAPIErrorRendersEveryFieldInAStableOrder(t *testing.T) {
	err := &APIError{
		Message: "Validation failed",
		Details: json.RawMessage(`{"fields":{"milestone":["unknown"],"epic":["no epic with that ref, key or slug"],"brief":["too long","must be text"]}}`),
	}
	got := err.Error()
	want := "Validation failed: brief: too long, must be text; epic: no epic with that ref, key or slug; milestone: unknown"
	if got != want {
		t.Fatalf("error = %q,\nwant        %q", got, want)
	}
}

// No detail, an empty one, or one that is not JSON at all must leave the
// message exactly as it was — a bad payload cannot be allowed to turn an error
// into noise.
func TestAPIErrorWithoutUsableDetailIsUnchanged(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"absent":    nil,
		"empty obj": json.RawMessage(`{}`),
		"null":      json.RawMessage(`null`),
		"no fields": json.RawMessage(`{"fields":{}}`),
	} {
		err := &APIError{Message: "Nope", Code: "conflict", Details: raw}
		if got, want := err.Error(), "Nope (code: conflict)"; got != want {
			t.Fatalf("%s: error = %q, want %q", name, got, want)
		}
	}
}

// A detail we do not model is passed through rather than dropped — but only
// while it stays short enough to belong on one line.
func TestAPIErrorPassesAnUnmodelledDetailThrough(t *testing.T) {
	short := &APIError{Message: "Nope", Details: json.RawMessage(`{"limit":5}`)}
	if got := short.Error(); !strings.Contains(got, `{"limit":5}`) {
		t.Fatalf("error = %q, want the unmodelled detail", got)
	}
	long := &APIError{Message: "Nope", Details: json.RawMessage(`{"blob":"` + strings.Repeat("x", 300) + `"}`)}
	if got := long.Error(); got != "Nope" {
		t.Fatalf("error = %q, want the long detail left off the line", got)
	}
}
