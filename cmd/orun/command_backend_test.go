package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cloudflare"
)

// cfEnvelope writes a standard Cloudflare success envelope.
func cfEnvelopeResp(w http.ResponseWriter, result interface{}) {
	type env struct {
		Success bool            `json:"success"`
		Errors  []interface{}   `json:"errors"`
		Result  json.RawMessage `json:"result"`
	}
	data, _ := json.Marshal(result)
	resp := env{Success: true, Errors: []interface{}{}, Result: data}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func setupFakeCFServer(t *testing.T, accountID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// D1 list/create.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/d1/database", accountID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelopeResp(w, []cloudflare.D1Database{})
			return
		}
		cfEnvelopeResp(w, cloudflare.D1Database{UUID: "fake-db-uuid", Name: "orun-db"})
	})

	// D1 query.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/d1/database/fake-db-uuid/query", accountID), func(w http.ResponseWriter, r *http.Request) {
		cfEnvelopeResp(w, []cloudflare.D1QueryResult{{Success: true, Results: []map[string]interface{}{}}})
	})

	// R2 list/create.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/r2/buckets", accountID), func(w http.ResponseWriter, r *http.Request) {
		type listResult struct {
			Buckets []cloudflare.R2Bucket `json:"buckets"`
		}
		if r.Method == http.MethodGet {
			cfEnvelopeResp(w, listResult{Buckets: []cloudflare.R2Bucket{}})
			return
		}
		cfEnvelopeResp(w, cloudflare.R2Bucket{Name: "orun-storage"})
	})

	// Worker upload.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/workers/scripts/orun-api", accountID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelopeResp(w, cloudflare.WorkerScript{ID: "orun-api"})
			return
		}
		if r.Method == http.MethodPut {
			cfEnvelopeResp(w, cloudflare.WorkerScript{ID: "orun-api"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Worker settings (vars).
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/workers/scripts/orun-api/settings", accountID), func(w http.ResponseWriter, r *http.Request) {
		cfEnvelopeResp(w, map[string]interface{}{})
	})

	// Worker secrets.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/workers/scripts/orun-api/secrets", accountID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelopeResp(w, []map[string]string{{"name": "ORUN_SESSION_SECRET", "type": "secret_text"}})
			return
		}
		cfEnvelopeResp(w, map[string]string{"name": "secret"})
	})

	// Worker subdomain.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/workers/subdomain", accountID), func(w http.ResponseWriter, r *http.Request) {
		cfEnvelopeResp(w, map[string]string{"subdomain": "testaccount"})
	})

	// Worker subdomain route.
	mux.HandleFunc(fmt.Sprintf("/accounts/%s/workers/scripts/orun-api/subdomain", accountID), func(w http.ResponseWriter, r *http.Request) {
		cfEnvelopeResp(w, map[string]interface{}{})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBackendInitDryRun(t *testing.T) {
	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set required env vars.
	t.Setenv(cfAccountIDEnvVar, "test-account")
	t.Setenv(cfAPITokenEnvVar, "test-token")

	initDryRun = true
	initJSON = false
	initName = "orun-api"
	initD1Name = "orun-db"
	initR2Bucket = "orun-storage"
	initOIDCAudience = "orun"
	initPublicURL = ""
	initDashboardURL = ""
	initGitHubClientID = ""
	initGitHubClientSecret = ""
	initSessionSecret = ""
	defer func() {
		initDryRun = false
		os.Stdout = old
	}()

	err := runBackendInit(context.Background())
	w.Close()
	buf.ReadFrom(r)
	os.Stdout = old

	if err != nil {
		t.Fatalf("dry-run init: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] in output, got: %s", out)
	}
	if !strings.Contains(out, "orun-api") {
		t.Errorf("expected worker name in output, got: %s", out)
	}
}

func TestBackendInitDryRunJSON(t *testing.T) {
	t.Setenv(cfAccountIDEnvVar, "test-account")
	t.Setenv(cfAPITokenEnvVar, "test-token")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	initDryRun = true
	initJSON = true
	initName = "orun-api"
	initD1Name = "orun-db"
	initR2Bucket = "orun-storage"
	initOIDCAudience = "orun"
	initPublicURL = ""
	initDashboardURL = ""
	initGitHubClientID = ""
	initGitHubClientSecret = ""
	initSessionSecret = ""
	defer func() {
		initDryRun = false
		initJSON = false
		os.Stdout = oldStdout
	}()

	err := runBackendInit(context.Background())
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("dry-run JSON init: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, buf.String())
	}
	if result["dryRun"] != true {
		t.Errorf("expected dryRun=true in JSON, got: %v", result["dryRun"])
	}
}

func TestBackendInitMissingCredentials(t *testing.T) {
	os.Unsetenv(cfAccountIDEnvVar)
	os.Unsetenv(cfAPITokenEnvVar)
	backendAccountID = ""
	backendAPIToken = ""
	initDryRun = false
	defer func() { initDryRun = false }()

	err := runBackendInit(context.Background())
	if err == nil {
		t.Fatal("expected error for missing credentials, got nil")
	}
	if !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
		t.Errorf("expected credential hint in error, got: %v", err)
	}
}

func TestBackendDestroyRefusesWithoutYes(t *testing.T) {
	destroyYes = false
	destroyDryRun = false
	defer func() {
		destroyYes = false
		destroyDryRun = false
	}()

	err := runBackendDestroy(context.Background())
	if err == nil {
		t.Fatal("expected error without --yes, got nil")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected --yes hint in error, got: %v", err)
	}
}

func TestBackendDestroyDryRun(t *testing.T) {
	// Write fake metadata so destroy has something to operate on.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	destroyDryRun = true
	destroyYes = false
	destroyAdopted = true // allow without managed metadata
	destroyName = "orun-api"
	defer func() {
		destroyDryRun = false
		destroyAdopted = false
		destroyName = ""
		os.Stdout = oldStdout
	}()

	err := runBackendDestroy(context.Background())
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("destroy dry-run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] in output, got: %s", out)
	}
}

func TestBackendStatusJSON(t *testing.T) {
	accountID := "test-account"
	srv := setupFakeCFServer(t, accountID)

	t.Setenv(cfAccountIDEnvVar, accountID)
	t.Setenv(cfAPITokenEnvVar, "test-token")

	// We test the JSON output formatting by checking the JSON structure.
	result := statusResult{
		WorkerReady:     true,
		WorkerName:      "orun-api",
		D1Ready:         true,
		D1DatabaseName:  "orun-db",
		R2Ready:         true,
		R2BucketName:    "orun-storage",
		MigrationsReady: true,
		BackendURL:      "https://orun-api.testaccount.workers.dev",
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = printJSON(result)
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = oldStdout

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}
	if parsed["workerReady"] != true {
		t.Errorf("expected workerReady=true, got: %v", parsed["workerReady"])
	}
	_ = srv
}

func TestOutputRedactsSecrets(t *testing.T) {
	// Verify that printJSON does not include secret fields.
	result := initResult{
		DryRun:     true,
		WorkerName: "orun-api",
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = printJSON(result)
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = oldStdout

	out := buf.String()
	secretPhrases := []string{
		"client_secret",
		"session_secret",
		"apiToken",
		"api_token",
		"ORUN_SESSION_SECRET",
		"GITHUB_CLIENT_SECRET",
	}
	for _, phrase := range secretPhrases {
		if strings.Contains(strings.ToLower(out), strings.ToLower(phrase)) {
			t.Errorf("output contains secret phrase %q: %s", phrase, out)
		}
	}
}
