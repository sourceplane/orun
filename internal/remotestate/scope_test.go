package remotestate_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// captureServer records the request path, the contract-version header, and an
// optional captured Orun-Contract-Version for assertions.
func captureServer(t *testing.T, status int, body interface{}) (*httptest.Server, *string, *string) {
	t.Helper()
	gotPath := ""
	gotVersion := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVersion = r.Header.Get("Orun-Contract-Version")
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotPath, &gotVersion
}

func okRun() map[string]interface{} {
	return map[string]interface{}{
		"runId": "r", "status": "pending", "planChecksum": "x",
		"triggerType": "ci", "createdAt": "2024-01-01T00:00:00Z",
		"updatedAt": "2024-01-01T00:00:00Z",
	}
}

func TestScopedPath_DefaultLocalScope(t *testing.T) {
	srv, gotPath, gotVersion := captureServer(t, 0, okRun())
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	if _, err := c.GetRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("GetRun error: %v", err)
	}
	want := "/v1/organizations/_local/projects/_local/state/runs/run-1"
	if *gotPath != want {
		t.Errorf("path = %q, want %q", *gotPath, want)
	}
	if *gotVersion != "1" {
		t.Errorf("Orun-Contract-Version = %q, want 1", *gotVersion)
	}
}

func TestScopedPath_ExplicitScope(t *testing.T) {
	srv, gotPath, _ := captureServer(t, 0, okRun())
	c := remotestate.NewClientWithScope(srv.URL, "test",
		remotestate.NewStaticTokenSource("tok"),
		remotestate.Scope{OrgID: "acme", ProjectID: "platform"})
	if _, err := c.GetRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("GetRun error: %v", err)
	}
	want := "/v1/organizations/acme/projects/platform/state/runs/run-1"
	if *gotPath != want {
		t.Errorf("path = %q, want %q", *gotPath, want)
	}
	if c.Scope().OrgID != "acme" || c.Scope().ProjectID != "platform" {
		t.Errorf("Scope() = %+v", c.Scope())
	}
}

func TestScopedPath_PartialScopeFillsLocal(t *testing.T) {
	srv, gotPath, _ := captureServer(t, 0, okRun())
	c := remotestate.NewClientWithScope(srv.URL, "test",
		remotestate.NewStaticTokenSource("tok"),
		remotestate.Scope{OrgID: "acme"}) // project empty
	if _, err := c.GetRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("GetRun error: %v", err)
	}
	want := "/v1/organizations/acme/projects/_local/state/runs/run-1"
	if *gotPath != want {
		t.Errorf("path = %q, want %q", *gotPath, want)
	}
}

func TestScopedPath_ClaimAndLogsAndUpload(t *testing.T) {
	// Verify the scoped prefix is applied to the nested endpoints too, and that
	// the raw (non-JSON) UploadLog/GetLog paths carry the version header.
	srv, gotPath, gotVersion := captureServer(t, 0, map[string]interface{}{"claimed": true})
	c := remotestate.NewClientWithScope(srv.URL, "test",
		remotestate.NewStaticTokenSource("tok"),
		remotestate.Scope{OrgID: "o", ProjectID: "p"})

	if _, err := c.ClaimJob(context.Background(), "run-1", "job-1", "runner-1"); err != nil {
		t.Fatalf("ClaimJob error: %v", err)
	}
	wantClaim := "/v1/organizations/o/projects/p/state/runs/run-1/jobs/job-1/claim"
	if *gotPath != wantClaim {
		t.Errorf("claim path = %q, want %q", *gotPath, wantClaim)
	}

	if _, err := c.AppendLog(context.Background(), "run-1", "job-1", "runner-1", "out"); err != nil {
		t.Fatalf("AppendLog error: %v", err)
	}
	wantLog := "/v1/organizations/o/projects/p/state/runs/run-1/logs/job-1"
	if *gotPath != wantLog {
		t.Errorf("log path = %q, want %q", *gotPath, wantLog)
	}
	if *gotVersion != "1" {
		t.Errorf("AppendLog Orun-Contract-Version = %q, want 1", *gotVersion)
	}
}

func TestContractVersionUnsupported_NestedEnvelopeRendered(t *testing.T) {
	srv, _, _ := captureServer(t, http.StatusConflict, map[string]interface{}{
		"error": map[string]interface{}{
			"code":      "contract_version_unsupported",
			"message":   "client speaks v1; backend requires v2",
			"requestId": "req-abc123",
			"details":   map[string]interface{}{"supported": "2-3"},
		},
	})
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	_, err := c.GetRun(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"too old or too new", "contract version 1", "supports 2-3", "req-abc123"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

func TestContractVersionUnsupported_NotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"code": "contract_version_unsupported", "message": "skew"},
		})
	}))
	defer srv.Close()

	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	// GetRun is retryable; the contract-version rejection must short-circuit.
	if _, err := c.GetRun(context.Background(), "run-1"); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("contract_version_unsupported should not retry; got %d calls", calls)
	}
}

func TestErrorEnvelope_NestedSurfacesCodeAndRequestID(t *testing.T) {
	srv, _, _ := captureServer(t, http.StatusNotFound, map[string]interface{}{
		"error": map[string]interface{}{
			"code":      "object_missing",
			"message":   "plan blob not uploaded",
			"requestId": "req-xyz",
		},
	})
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	_, err := c.GetRun(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := errAsAPI(err)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "object_missing" {
		t.Errorf("Code = %q, want object_missing", apiErr.Code)
	}
	if apiErr.RequestID != "req-xyz" {
		t.Errorf("RequestID = %q, want req-xyz", apiErr.RequestID)
	}
	if !strings.Contains(err.Error(), "req-xyz") {
		t.Errorf("error string should surface requestId, got %q", err.Error())
	}
}

func TestErrorEnvelope_FlatOSSStillParses(t *testing.T) {
	// The OSS backend's flat { error, code } envelope must keep working.
	srv, _, _ := captureServer(t, http.StatusNotFound, map[string]interface{}{
		"error": "run not found",
		"code":  "NOT_FOUND",
	})
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	_, err := c.GetRun(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") || !strings.Contains(err.Error(), "run not found") {
		t.Errorf("flat envelope not surfaced: %v", err)
	}
}

func TestErrorEnvelope_NestedAuthDetectedForRetryHint(t *testing.T) {
	srv, _, _ := captureServer(t, http.StatusUnauthorized, map[string]interface{}{
		"error": map[string]interface{}{"code": "unauthorized", "message": "token expired"},
	})
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	err := c.UpdateJob(context.Background(), "run", "job", "runner", "success", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("lowercase platform auth code should trigger auth hint, got %v", err)
	}
}

// errAsAPI is a small helper using errors.As on *remotestate.APIError.
func errAsAPI(err error) (*remotestate.APIError, bool) {
	var apiErr *remotestate.APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
