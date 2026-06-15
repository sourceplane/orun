package remotestate

import (
	"testing"
)

// decodeSuccessBody must unwrap the platform { data, meta } success envelope and
// fall back to a flat decode for the OSS single-tenant backend (no envelope).
func TestDecodeSuccessBody(t *testing.T) {
	type run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

	t.Run("unwraps the platform data envelope", func(t *testing.T) {
		body := []byte(`{"data":{"id":"run_123","status":"running"},"meta":{"requestId":"req_1","cursor":null}}`)
		var got run
		if err := decodeSuccessBody(body, &got); err != nil {
			t.Fatalf("decodeSuccessBody: %v", err)
		}
		if got.ID != "run_123" || got.Status != "running" {
			t.Fatalf("got %+v, want {run_123 running} (envelope not unwrapped)", got)
		}
	})

	t.Run("falls back to a flat body (OSS backend, no envelope)", func(t *testing.T) {
		body := []byte(`{"id":"run_456","status":"queued"}`)
		var got run
		if err := decodeSuccessBody(body, &got); err != nil {
			t.Fatalf("decodeSuccessBody: %v", err)
		}
		if got.ID != "run_456" || got.Status != "queued" {
			t.Fatalf("got %+v, want {run_456 queued}", got)
		}
	})

	t.Run("decodes a bare array body as-is", func(t *testing.T) {
		body := []byte(`[{"id":"a","status":"x"},{"id":"b","status":"y"}]`)
		var got []run
		if err := decodeSuccessBody(body, &got); err != nil {
			t.Fatalf("decodeSuccessBody: %v", err)
		}
		if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
			t.Fatalf("got %+v, want 2 elements a,b", got)
		}
	})

	t.Run("empty body is a no-op", func(t *testing.T) {
		var got run
		if err := decodeSuccessBody(nil, &got); err != nil {
			t.Fatalf("decodeSuccessBody(nil): %v", err)
		}
		if got != (run{}) {
			t.Fatalf("got %+v, want zero value", got)
		}
	})
}
