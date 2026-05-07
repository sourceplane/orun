package cloudflare_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cloudflare"
)

func newTestClient(t *testing.T, mux *http.ServeMux) *cloudflare.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return cloudflare.New(cloudflare.Options{
		AccountID:  "test-account",
		APIToken:   "test-token",
		BaseURL:    srv.URL,
		UserAgent:  "orun-cli/test",
		HTTPClient: srv.Client(),
	})
}

func cfEnvelope(t *testing.T, w http.ResponseWriter, result interface{}) {
	t.Helper()
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

func cfError(t *testing.T, w http.ResponseWriter, code int, msg string) {
	t.Helper()
	type cfErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type env struct {
		Success bool    `json:"success"`
		Errors  []cfErr `json:"errors"`
	}
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(env{Success: false, Errors: []cfErr{{Code: code, Message: msg}}})
}

// TestAuthHeaderAndUserAgent verifies that all requests carry the Authorization header and User-Agent.
func TestAuthHeaderAndUserAgent(t *testing.T) {
	gotAuth := ""
	gotUA := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		cfEnvelope(t, w, []interface{}{})
	})
	client := newTestClient(t, mux)
	_, err := client.ListD1Databases(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth header = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotUA != "orun-cli/test" {
		t.Errorf("user-agent = %q, want %q", gotUA, "orun-cli/test")
	}
}

// TestListD1 verifies listing D1 databases.
func TestListD1(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database", func(w http.ResponseWriter, r *http.Request) {
		cfEnvelope(t, w, []cloudflare.D1Database{
			{UUID: "uuid-1", Name: "orun-db"},
			{UUID: "uuid-2", Name: "other-db"},
		})
	})
	client := newTestClient(t, mux)
	dbs, err := client.ListD1Databases(context.Background())
	if err != nil {
		t.Fatalf("ListD1Databases: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}
}

// TestCreateD1Idempotent verifies that CreateD1Database returns existing DB without creating duplicate.
func TestCreateD1Idempotent(t *testing.T) {
	createCalled := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.D1Database{{UUID: "existing-uuid", Name: "orun-db"}})
			return
		}
		// POST should not be called when DB already exists.
		createCalled++
		cfEnvelope(t, w, cloudflare.D1Database{UUID: "new-uuid", Name: "orun-db"})
	})
	client := newTestClient(t, mux)
	db, err := client.CreateD1Database(context.Background(), "orun-db")
	if err != nil {
		t.Fatalf("CreateD1Database: %v", err)
	}
	if db.UUID != "existing-uuid" {
		t.Errorf("expected existing-uuid, got %s", db.UUID)
	}
	if createCalled != 0 {
		t.Errorf("POST called %d times, expected 0 (should reuse existing)", createCalled)
	}
}

// TestCreateD1New verifies creating a new D1 database.
func TestCreateD1New(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.D1Database{})
			return
		}
		cfEnvelope(t, w, cloudflare.D1Database{UUID: "new-uuid", Name: "orun-db"})
	})
	client := newTestClient(t, mux)
	db, err := client.CreateD1Database(context.Background(), "orun-db")
	if err != nil {
		t.Fatalf("CreateD1Database: %v", err)
	}
	if db.UUID != "new-uuid" {
		t.Errorf("expected new-uuid, got %s", db.UUID)
	}
}

// TestListR2 verifies listing R2 buckets.
func TestListR2(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/r2/buckets", func(w http.ResponseWriter, r *http.Request) {
		type listResult struct {
			Buckets []cloudflare.R2Bucket `json:"buckets"`
		}
		cfEnvelope(t, w, listResult{Buckets: []cloudflare.R2Bucket{{Name: "orun-storage"}}})
	})
	client := newTestClient(t, mux)
	buckets, err := client.ListR2Buckets(context.Background())
	if err != nil {
		t.Fatalf("ListR2Buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "orun-storage" {
		t.Fatalf("unexpected buckets: %+v", buckets)
	}
}

// TestCreateR2Idempotent verifies that CreateR2Bucket returns existing bucket without creating duplicate.
func TestCreateR2Idempotent(t *testing.T) {
	createCalled := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/r2/buckets", func(w http.ResponseWriter, r *http.Request) {
		type listResult struct {
			Buckets []cloudflare.R2Bucket `json:"buckets"`
		}
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, listResult{Buckets: []cloudflare.R2Bucket{{Name: "orun-storage"}}})
			return
		}
		createCalled++
		cfEnvelope(t, w, cloudflare.R2Bucket{Name: "orun-storage"})
	})
	client := newTestClient(t, mux)
	bucket, err := client.CreateR2Bucket(context.Background(), "orun-storage")
	if err != nil {
		t.Fatalf("CreateR2Bucket: %v", err)
	}
	if bucket.Name != "orun-storage" {
		t.Errorf("unexpected bucket name: %s", bucket.Name)
	}
	if createCalled != 0 {
		t.Errorf("POST called %d times, expected 0", createCalled)
	}
}

// TestSetWorkerSecretRedactsValueInError verifies that secret values are not surfaced in errors.
func TestSetWorkerSecretRedactsValueInError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/workers/scripts/orun-api/secrets", func(w http.ResponseWriter, r *http.Request) {
		cfError(t, w, 10000, "internal error")
	})
	client := newTestClient(t, mux)
	err := client.SetWorkerSecret(context.Background(), "orun-api", "ORUN_SESSION_SECRET", "super-secret-value-never-log-this")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr := err.Error()
	if strings.Contains(errStr, "super-secret-value-never-log-this") {
		t.Errorf("error contains secret value: %s", errStr)
	}
}

// TestAPIErrorEnvelope verifies that Cloudflare error envelopes are parsed correctly.
func TestAPIErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database", func(w http.ResponseWriter, r *http.Request) {
		cfError(t, w, 10001, "authentication required")
	})
	client := newTestClient(t, mux)
	_, err := client.ListD1Databases(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected auth error in message, got: %s", err.Error())
	}
}

// TestGetWorkerScriptMissing verifies that a missing Worker returns nil, not error.
func TestGetWorkerScriptMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/workers/scripts/orun-api", func(w http.ResponseWriter, r *http.Request) {
		type cfErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		type env struct {
			Success bool    `json:"success"`
			Errors  []cfErr `json:"errors"`
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(env{Success: false, Errors: []cfErr{{Code: 10007, Message: "not found"}}})
	})
	client := newTestClient(t, mux)
	script, err := client.GetWorkerScript(context.Background(), "orun-api")
	if err != nil {
		t.Fatalf("unexpected error for missing Worker: %v", err)
	}
	if script != nil {
		t.Errorf("expected nil for missing Worker, got %+v", script)
	}
}

// TestMigrationsAppliedInOrder verifies D1 SQL execution order via multiple calls.
func TestMigrationsAppliedInOrder(t *testing.T) {
	var sqlCalls []string
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/d1/database/test-db-uuid/query", func(w http.ResponseWriter, r *http.Request) {
		type queryReq []struct {
			SQL string `json:"sql"`
		}
		var body queryReq
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) > 0 {
			sqlCalls = append(sqlCalls, body[0].SQL)
		}
		cfEnvelope(t, w, []cloudflare.D1QueryResult{{Success: true}})
	})
	client := newTestClient(t, mux)
	sqls := []string{"CREATE TABLE a (id TEXT);", "CREATE TABLE b (id TEXT);"}
	for _, sql := range sqls {
		_, err := client.ExecD1SQL(context.Background(), "test-db-uuid", sql)
		if err != nil {
			t.Fatalf("ExecD1SQL: %v", err)
		}
	}
	if len(sqlCalls) != 2 {
		t.Errorf("expected 2 SQL calls, got %d", len(sqlCalls))
	}
	if sqlCalls[0] != sqls[0] || sqlCalls[1] != sqls[1] {
		t.Errorf("SQL calls not in order: %v", sqlCalls)
	}
}
