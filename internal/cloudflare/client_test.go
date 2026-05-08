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

// ── Queue tests ───────────────────────────────────────────────────────────────

// TestListQueues verifies listing queues.
func TestListQueues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues", func(w http.ResponseWriter, r *http.Request) {
		cfEnvelope(t, w, []cloudflare.Queue{
			{QueueID: "q1", QueueName: "orun-catalog-ingest"},
			{QueueID: "q2", QueueName: "orun-catalog-ingest-dlq"},
		})
	})
	client := newTestClient(t, mux)
	queues, err := client.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues: %v", err)
	}
	if len(queues) != 2 {
		t.Fatalf("expected 2 queues, got %d", len(queues))
	}
}

// TestCreateQueueIdempotent verifies that CreateQueue reuses an existing queue without creating a duplicate.
func TestCreateQueueIdempotent(t *testing.T) {
	createCalled := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.Queue{{QueueID: "existing-q", QueueName: "orun-catalog-ingest"}})
			return
		}
		createCalled++
		cfEnvelope(t, w, cloudflare.Queue{QueueID: "new-q", QueueName: "orun-catalog-ingest"})
	})
	client := newTestClient(t, mux)
	q, err := client.CreateQueue(context.Background(), "orun-catalog-ingest")
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	if q.QueueID != "existing-q" {
		t.Errorf("expected existing-q, got %s", q.QueueID)
	}
	if createCalled != 0 {
		t.Errorf("POST called %d times, expected 0 (should reuse existing)", createCalled)
	}
}

// TestCreateQueueNew verifies creating a new queue when none exists.
func TestCreateQueueNew(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.Queue{})
			return
		}
		cfEnvelope(t, w, cloudflare.Queue{QueueID: "new-q", QueueName: "orun-catalog-ingest"})
	})
	client := newTestClient(t, mux)
	q, err := client.CreateQueue(context.Background(), "orun-catalog-ingest")
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	if q.QueueID != "new-q" {
		t.Errorf("expected new-q, got %s", q.QueueID)
	}
}

// TestDeleteQueueByNameMissing verifies that deleting a non-existent queue is not an error.
func TestDeleteQueueByNameMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues", func(w http.ResponseWriter, r *http.Request) {
		cfEnvelope(t, w, []cloudflare.Queue{})
	})
	client := newTestClient(t, mux)
	err := client.DeleteQueueByName(context.Background(), "nonexistent-queue")
	if err != nil {
		t.Fatalf("expected no error for missing queue, got: %v", err)
	}
}

// TestDeleteQueueByIDMissing verifies that deleting a non-existent queue by ID is not an error.
func TestDeleteQueueByIDMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues/gone-q", func(w http.ResponseWriter, r *http.Request) {
		cfError(t, w, 404, "not found")
	})
	client := newTestClient(t, mux)
	err := client.DeleteQueueByID(context.Background(), "gone-q")
	if err != nil {
		t.Fatalf("expected no error for missing queue, got: %v", err)
	}
}

// ── Queue consumer tests ──────────────────────────────────────────────────────

// TestCreateQueueConsumerNew verifies creating a new consumer when none exists.
func TestCreateQueueConsumerNew(t *testing.T) {
	consumerCreated := false
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues/q1/consumers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.QueueConsumer{})
			return
		}
		consumerCreated = true
		cfEnvelope(t, w, cloudflare.QueueConsumer{ConsumerID: "c1", ScriptName: "orun-api", Type: "worker"})
	})
	client := newTestClient(t, mux)
	err := client.CreateOrUpdateQueueConsumer(context.Background(), "q1", "orun-api", "orun-catalog-ingest-dlq",
		cloudflare.QueueConsumerSettings{BatchSize: 10, MaxRetries: 3, MaxWaitTimeMs: 30000})
	if err != nil {
		t.Fatalf("CreateOrUpdateQueueConsumer: %v", err)
	}
	if !consumerCreated {
		t.Error("expected consumer to be created, POST not called")
	}
}

// TestCreateQueueConsumerIdempotent verifies no-op when consumer exists with matching settings.
func TestCreateQueueConsumerIdempotent(t *testing.T) {
	postCalled := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues/q1/consumers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.QueueConsumer{{
				ConsumerID:      "c1",
				ScriptName:      "orun-api",
				Type:            "worker",
				DeadLetterQueue: "orun-catalog-ingest-dlq",
				Settings:        cloudflare.QueueConsumerSettings{BatchSize: 10, MaxRetries: 3, MaxWaitTimeMs: 30000},
			}})
			return
		}
		postCalled++
		cfEnvelope(t, w, map[string]interface{}{})
	})
	client := newTestClient(t, mux)
	err := client.CreateOrUpdateQueueConsumer(context.Background(), "q1", "orun-api", "orun-catalog-ingest-dlq",
		cloudflare.QueueConsumerSettings{BatchSize: 10, MaxRetries: 3, MaxWaitTimeMs: 30000})
	if err != nil {
		t.Fatalf("CreateOrUpdateQueueConsumer: %v", err)
	}
	if postCalled != 0 {
		t.Errorf("POST called %d times, expected 0 for idempotent no-op", postCalled)
	}
}

// TestCreateQueueConsumerReplacesOnSettingsMismatch verifies delete+recreate when settings differ.
func TestCreateQueueConsumerReplacesOnSettingsMismatch(t *testing.T) {
	deleteCalled := false
	postCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues/q1/consumers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfEnvelope(t, w, []cloudflare.QueueConsumer{{
				ConsumerID:      "c1",
				ScriptName:      "orun-api",
				Type:            "worker",
				DeadLetterQueue: "orun-catalog-ingest-dlq",
				Settings:        cloudflare.QueueConsumerSettings{BatchSize: 5, MaxRetries: 1, MaxWaitTimeMs: 10000},
			}})
			return
		}
		postCalled = true
		cfEnvelope(t, w, map[string]interface{}{})
	})
	mux.HandleFunc("/accounts/test-account/queues/q1/consumers/c1", func(w http.ResponseWriter, r *http.Request) {
		deleteCalled = true
		cfEnvelope(t, w, map[string]interface{}{})
	})
	client := newTestClient(t, mux)
	err := client.CreateOrUpdateQueueConsumer(context.Background(), "q1", "orun-api", "orun-catalog-ingest-dlq",
		cloudflare.QueueConsumerSettings{BatchSize: 10, MaxRetries: 3, MaxWaitTimeMs: 30000})
	if err != nil {
		t.Fatalf("CreateOrUpdateQueueConsumer: %v", err)
	}
	if !deleteCalled {
		t.Error("expected old consumer to be deleted")
	}
	if !postCalled {
		t.Error("expected new consumer to be created")
	}
}

// TestDeleteQueueConsumerMissing verifies that deleting a non-existent consumer is not an error.
func TestDeleteQueueConsumerMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/queues/q1/consumers/gone", func(w http.ResponseWriter, r *http.Request) {
		cfError(t, w, 404, "not found")
	})
	client := newTestClient(t, mux)
	err := client.DeleteQueueConsumer(context.Background(), "q1", "gone")
	if err != nil {
		t.Fatalf("expected no error for missing consumer, got: %v", err)
	}
}

// ── Worker schedule tests ─────────────────────────────────────────────────────

// TestUpdateWorkerSchedules verifies that the PUT request body is a JSON array of cron objects.
func TestUpdateWorkerSchedules(t *testing.T) {
	var capturedCrons []string
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/workers/scripts/orun-api/schedules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			type entry struct {
				Cron string `json:"cron"`
			}
			var body []entry
			json.NewDecoder(r.Body).Decode(&body)
			for _, e := range body {
				capturedCrons = append(capturedCrons, e.Cron)
			}
			type result struct {
				Schedules []entry `json:"schedules"`
			}
			cfEnvelope(t, w, result{Schedules: body})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	client := newTestClient(t, mux)
	err := client.UpdateWorkerSchedules(context.Background(), "orun-api", []string{"*/15 * * * *"})
	if err != nil {
		t.Fatalf("UpdateWorkerSchedules: %v", err)
	}
	if len(capturedCrons) != 1 || capturedCrons[0] != "*/15 * * * *" {
		t.Errorf("expected cron [*/15 * * * *], got %v", capturedCrons)
	}
}

// TestDeleteWorkerSchedulesClearsAll verifies that DeleteWorkerSchedules sends an empty array.
func TestDeleteWorkerSchedulesClearsAll(t *testing.T) {
	var bodyWasEmpty bool
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/workers/scripts/orun-api/schedules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			type entry struct {
				Cron string `json:"cron"`
			}
			var body []entry
			json.NewDecoder(r.Body).Decode(&body)
			bodyWasEmpty = len(body) == 0
			type result struct {
				Schedules []entry `json:"schedules"`
			}
			cfEnvelope(t, w, result{Schedules: body})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	client := newTestClient(t, mux)
	err := client.DeleteWorkerSchedules(context.Background(), "orun-api")
	if err != nil {
		t.Fatalf("DeleteWorkerSchedules: %v", err)
	}
	if !bodyWasEmpty {
		t.Error("expected empty array body to clear schedules")
	}
}

// ── Worker upload binding tests ───────────────────────────────────────────────

// TestUploadWorkerIncludesQueueBinding verifies that UploadWorkerScript sends a queue producer binding.
func TestUploadWorkerIncludesQueueBinding(t *testing.T) {
	var capturedBindingTypes []string
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/test-account/workers/scripts/orun-api", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_ = r.ParseMultipartForm(10 << 20)
			metaPart := r.FormValue("metadata")
			type binding struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			type meta struct {
				Bindings []binding `json:"bindings"`
			}
			var m meta
			json.Unmarshal([]byte(metaPart), &m)
			for _, b := range m.Bindings {
				capturedBindingTypes = append(capturedBindingTypes, b.Type)
			}
			cfEnvelope(t, w, cloudflare.WorkerScript{ID: "orun-api"})
			return
		}
		cfEnvelope(t, w, cloudflare.WorkerScript{ID: "orun-api"})
	})
	client := newTestClient(t, mux)
	bindings := []cloudflare.WorkerBinding{
		{Type: "durable_object_namespace", Name: "COORDINATOR", ClassName: "RunCoordinator", ScriptName: "orun-api"},
		{Type: "d1", Name: "DB", DatabaseID: "db-uuid"},
		{Type: "r2_bucket", Name: "STORAGE", BucketName: "orun-storage"},
		{Type: "queue", Name: "CATALOG_INGEST_QUEUE", QueueName: "orun-catalog-ingest"},
		{Type: "plain_text", Name: "GITHUB_OIDC_AUDIENCE", Text: "orun"},
	}
	_, err := client.UploadWorkerScript(context.Background(), cloudflare.UploadWorkerParams{
		ScriptName: "orun-api",
		Bundle:     []byte("export default {};"),
		Bindings:   bindings,
	})
	if err != nil {
		t.Fatalf("UploadWorkerScript: %v", err)
	}
	wantTypes := map[string]bool{
		"durable_object_namespace": false,
		"d1":                       false,
		"r2_bucket":                false,
		"queue":                    false,
		"plain_text":               false,
	}
	for _, bt := range capturedBindingTypes {
		wantTypes[bt] = true
	}
	for bt, found := range wantTypes {
		if !found {
			t.Errorf("binding type %q not found in upload metadata — bindings present: %v", bt, capturedBindingTypes)
		}
	}
}
