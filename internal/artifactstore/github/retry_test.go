package github

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/runbundle"
)

// --- Retry tests ---

func TestBackoffDuration(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:   3,
		InitialDelay: time.Second,
		MaxDelay:     10 * time.Second,
		JitterFactor: 0, // deterministic
	}

	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{1, time.Second, time.Second + time.Millisecond},          // 1s
		{2, 2 * time.Second, 2*time.Second + time.Millisecond},    // 2s
		{3, 4 * time.Second, 4*time.Second + time.Millisecond},    // 4s
		{4, 8 * time.Second, 8*time.Second + time.Millisecond},    // 8s (clamped to max)
		{5, 10 * time.Second, 10*time.Second + time.Millisecond},  // 10s (at max)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			d := backoffDuration(tt.attempt, cfg)
			if d < tt.wantMin || d > tt.wantMax {
				t.Errorf("backoffDuration(%d) = %v, want between %v and %v",
					tt.attempt, d, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestBackoffDuration_Jitter(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:   3,
		InitialDelay: time.Second,
		MaxDelay:     10 * time.Second,
		JitterFactor: 0.25,
	}

	// With jitter, delays should vary but stay within [0.75*base, 1.25*base]
	base := time.Second
	for attempt := 1; attempt <= 3; attempt++ {
		d := backoffDuration(attempt, cfg)
		expected := time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))
		if expected > cfg.MaxDelay {
			expected = cfg.MaxDelay
		}
		lower := time.Duration(float64(expected) * (1 - cfg.JitterFactor))
		upper := time.Duration(float64(expected) * (1 + cfg.JitterFactor))

		if d < lower || d > upper {
			t.Errorf("attempt %d: backoff = %v, want between %v and %v",
				attempt, d, lower, upper)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{name: "nil", err: nil, retry: false},
		{name: "connection refused", err: fmt.Errorf("connection refused"), retry: true},
		{name: "connection reset by peer", err: fmt.Errorf("connection reset by peer"), retry: true},
		{name: "no such host", err: fmt.Errorf("no such host"), retry: true},
		{name: "tls handshake timeout", err: fmt.Errorf("tls handshake timeout"), retry: true},
		{name: "i/o timeout", err: fmt.Errorf("i/o timeout"), retry: true},
		{name: "read: connection reset", err: fmt.Errorf("read: connection reset"), retry: true},
		{name: "write: broken pipe", err: fmt.Errorf("write: broken pipe"), retry: true},
		{name: "use of closed network connection", err: fmt.Errorf("use of closed network connection"), retry: true},
		{name: "server closed connection", err: fmt.Errorf("server closed connection"), retry: true},
		{name: "timeout awaiting response headers", err: fmt.Errorf("timeout awaiting response headers"), retry: true},
		{name: "404 not found", err: fmt.Errorf("404 not found"), retry: false},
		{name: "permission denied", err: fmt.Errorf("permission denied"), retry: false},
		{name: "invalid request body", err: fmt.Errorf("invalid request body"), retry: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryableError(tt.err)
			if got != tt.retry {
				t.Errorf("IsRetryableError(%q) = %v, want %v", tt.err, got, tt.retry)
			}
		})
	}
}

func TestIsUploadRetryable(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{name: "nil", err: nil, retry: false},
		{name: "connection refused", err: fmt.Errorf("connection refused"), retry: true},
		{name: "request timeout", err: fmt.Errorf("request timeout"), retry: true},
		{name: "artifact upload failed", err: fmt.Errorf("artifact upload failed"), retry: true},
		{name: "network error during upload", err: fmt.Errorf("network error during upload"), retry: true},
		{name: "ECONNRESET", err: fmt.Errorf("ECONNRESET"), retry: true},
		{name: "ECONNREFUSED", err: fmt.Errorf("ECONNREFUSED"), retry: true},
		{name: "ETIMEDOUT", err: fmt.Errorf("ETIMEDOUT"), retry: true},
		{name: "ENOTFOUND", err: fmt.Errorf("ENOTFOUND"), retry: true},
		{name: "the operation timed out", err: fmt.Errorf("the operation timed out"), retry: true},
		{name: "non-retryable: invalid shard", err: fmt.Errorf("non-retryable: invalid shard"), retry: false},
		{name: "shard directory is required", err: fmt.Errorf("shard directory is required"), retry: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUploadRetryable(tt.err)
			if got != tt.retry {
				t.Errorf("isUploadRetryable(%q) = %v, want %v", tt.err, got, tt.retry)
			}
		})
	}
}

func TestRetryConfig_IsRetryableStatus(t *testing.T) {
	cfg := RetryConfig{
		RetryableStatus: []int{429, 500, 502, 503, 504},
	}

	tests := []struct {
		status int
		retry  bool
	}{
		{200, false},
		{201, false},
		{301, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			got := cfg.isRetryableStatus(tt.status)
			if got != tt.retry {
				t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.status, got, tt.retry)
			}
		})
	}
}

// --- HTTP retry integration tests ---

func TestClient_RetryDo_SuccessAfterRetry(t *testing.T) {
	ctx := context.Background()
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client, _ := NewClient(ctx, "owner/repo",
		WithBaseURL(server.URL),
		WithToken("test-token"),
		WithRetryConfig(RetryConfig{
			MaxRetries:      4,
			InitialDelay:    time.Millisecond,
			MaxDelay:        50 * time.Millisecond,
			JitterFactor:    0,
			RetryableStatus: []int{503},
		}),
	)

	req, _ := client.newRequest(ctx, server.URL+"/test")
	resp, err := client.doRequest(req)
	if err != nil {
		t.Fatalf("doRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", attempts)
	}
}

func TestClient_RetryDo_ExhaustsRetries(t *testing.T) {
	ctx := context.Background()
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewClient(ctx, "owner/repo",
		WithBaseURL(server.URL),
		WithToken("test-token"),
		WithRetryConfig(RetryConfig{
			MaxRetries:      2,
			InitialDelay:    time.Millisecond,
			MaxDelay:        10 * time.Millisecond,
			JitterFactor:    0,
			RetryableStatus: []int{500},
		}),
	)

	req, _ := client.newRequest(ctx, server.URL+"/test")
	_, err := client.doRequest(req)
	if err == nil {
		t.Fatal("expected error from exhausted retries")
	}

	// MaxRetries=2 means initial attempt + 2 retries = 3 total
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestClient_WithRetryConfig(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxRetries:      5,
		InitialDelay:    time.Second,
		MaxDelay:        30 * time.Second,
		JitterFactor:    0.1,
		RetryableStatus: []int{429, 500},
	}

	client, _ := NewClient(ctx, "owner/repo",
		WithToken("test"),
		WithRetryConfig(cfg),
	)

	if client.retryConfig.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", client.retryConfig.MaxRetries)
	}
	if client.retryConfig.JitterFactor != 0.1 {
		t.Errorf("JitterFactor = %f, want 0.1", client.retryConfig.JitterFactor)
	}
}

// --- Package tests ---

func TestPackageShardAsZip(t *testing.T) {
	shardDir := t.TempDir()

	manifestContent := `{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard","role":"plan","execId":"gh-123-1-abc"}`
	if err := os.WriteFile(filepath.Join(shardDir, "manifest.json"), []byte(manifestContent), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shardDir, "plan.json"), []byte(`{"plan":"test"}`), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	zipData, totalSize, err := PackageShardAsZip(shardDir)
	if err != nil {
		t.Fatalf("PackageShardAsZip failed: %v", err)
	}

	if len(zipData) == 0 {
		t.Fatal("zip data is empty")
	}
	if totalSize <= 0 {
		t.Errorf("totalSize = %d, want > 0", totalSize)
	}
}

func TestPackageShardAsZip_EmptyDir(t *testing.T) {
	_, _, err := PackageShardAsZip("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestPackageShardAsZip_MissingManifest(t *testing.T) {
	shardDir := t.TempDir()
	_, _, err := PackageShardAsZip(shardDir)
	if err == nil {
		t.Fatal("expected error for missing manifest.json")
	}
}

func TestPackageShardAsZip_IncludesManifest(t *testing.T) {
	shardDir := t.TempDir()

	manifestContent := `{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard","role":"plan","execId":"test-123"}`
	if err := os.WriteFile(filepath.Join(shardDir, "manifest.json"), []byte(manifestContent), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shardDir, "plan.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shardDir, "checksums.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}

	zipData, _, err := PackageShardAsZip(shardDir)
	if err != nil {
		t.Fatalf("PackageShardAsZip failed: %v", err)
	}

	// Verify the zip contains manifest.json
	if !strings.Contains(string(zipData), "manifest.json") {
		t.Error("zip should contain manifest.json")
	}
}

// --- resolveRunIDFromShard tests ---

func TestResolveRunIDFromShard_FromExecID(t *testing.T) {
	shard := &runbundle.Shard{
		ExecID: "gh-12345-1-a1b2c3d4",
	}

	runID, err := resolveRunIDFromShard(shard)
	if err != nil {
		t.Fatalf("resolveRunIDFromShard failed: %v", err)
	}
	if runID != 12345 {
		t.Errorf("runID = %d, want 12345", runID)
	}
}

func TestResolveRunIDFromShard_FromSource(t *testing.T) {
	shard := &runbundle.Shard{
		ExecID: "custom-exec-123",
		Manifest: &runbundle.RunBundleShardManifest{
			Source: runbundle.ShardSource{
				RunID: "67890",
			},
		},
	}

	runID, err := resolveRunIDFromShard(shard)
	if err != nil {
		t.Fatalf("resolveRunIDFromShard failed: %v", err)
	}
	if runID != 67890 {
		t.Errorf("runID = %d, want 67890", runID)
	}
}

func TestResolveRunIDFromShard_NoSource(t *testing.T) {
	shard := &runbundle.Shard{
		ExecID: "not-gh-format",
	}

	_, err := resolveRunIDFromShard(shard)
	if err == nil {
		t.Fatal("expected error for shard with no run ID source")
	}
}

func TestResolveRunIDFromShard_NonNumericExecID(t *testing.T) {
	shard := &runbundle.Shard{
		ExecID: "gh-abc-1-a1b2c3d4",
	}

	_, err := resolveRunIDFromShard(shard)
	if err == nil {
		t.Fatal("expected error for non-numeric run ID in exec ID")
	}
}

// --- VerifyArtifactExists tests ---

func TestVerifyArtifactExists_Found(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/42/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{"id": 1, "name": "orun.v1.gh-42-1-abc.plan.abc.created", "size_in_bytes": 100},
				},
			})
		},
	})
	defer server.Close()

	oldTimeout := UploadPollTimeout
	UploadPollTimeout = time.Second
	defer func() { UploadPollTimeout = oldTimeout }()

	err := client.VerifyArtifactExists(ctx, 42, "orun.v1.gh-42-1-abc.plan.abc.created")
	if err != nil {
		t.Fatalf("VerifyArtifactExists failed: %v", err)
	}
}

func TestVerifyArtifactExists_NotFound(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/99/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{},
			})
		},
	})
	defer server.Close()

	oldTimeout := UploadPollTimeout
	oldInterval := UploadPollInterval
	UploadPollTimeout = 100 * time.Millisecond
	UploadPollInterval = 10 * time.Millisecond
	defer func() {
		UploadPollTimeout = oldTimeout
		UploadPollInterval = oldInterval
	}()

	err := client.VerifyArtifactExists(ctx, 99, "nonexistent-artifact")
	if err == nil {
		t.Fatal("expected error for nonexistent artifact")
	}
	if !strings.Contains(err.Error(), "nonexistent-artifact") {
		t.Errorf("error should mention artifact name: %v", err)
	}
}

func TestVerifyArtifactExists_EmptyName(t *testing.T) {
	ctx := context.Background()
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	err := client.VerifyArtifactExists(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error for empty artifact name")
	}
}

// --- UploadShard tests (outside GHA) ---

func TestUploadShard_RESTAPI(t *testing.T) {
	ctx := context.Background()
	os.Unsetenv("GITHUB_ACTIONS")

	shardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shardDir, "manifest.json"), []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard","role":"job","execId":"gh-42-1-abc","planId":"abc123","files":{}}`), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var createCalled bool
	var uploadCalled bool

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/artifacts") && r.Method == http.MethodPost:
			createCalled = true
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":         99,
				"name":       "orun.v1.gh-42-1-abc.job.uid.completed",
				"size":       100,
				"upload_url": server.URL + "/upload/99",
			})
		case strings.Contains(r.URL.Path, "/upload/99") && r.Method == http.MethodPut:
			uploadCalled = true
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/artifacts") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{"id": 99, "name": "orun.v1.gh-42-1-abc.job.uid.completed", "size_in_bytes": 100},
				},
			})
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(ctx, "sourceplane/orun",
		WithBaseURL(server.URL),
		WithToken("test-token"),
		WithRetryConfig(RetryConfig{MaxRetries: 0}),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	shard := &runbundle.Shard{
		Dir:      shardDir,
		ExecID:   "gh-42-1-abc",
		Role:     runbundle.ShardRoleJob,
		Suffix:   "uid",
		Status:   "completed",
		Manifest: &runbundle.RunBundleShardManifest{},
	}

	oldTimeout := UploadPollTimeout
	UploadPollTimeout = time.Second
	defer func() { UploadPollTimeout = oldTimeout }()

	result, err := client.UploadShard(ctx, shard)
	if err != nil {
		t.Fatalf("UploadShard failed: %v", err)
	}

	if !createCalled {
		t.Error("artifact create endpoint was not called")
	}
	if !uploadCalled {
		t.Error("artifact upload endpoint was not called")
	}
	if result.ID != "99" {
		t.Errorf("result.ID = %q, want %q", result.ID, "99")
	}
	if !strings.Contains(result.Name, "orun.v1.gh-42-1-abc") {
		t.Errorf("result.Name = %q, should contain exec ID", result.Name)
	}
}

func TestUploadShard_NilShard(t *testing.T) {
	ctx := context.Background()
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	_, err := client.UploadShard(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil shard")
	}
}

func TestUploadShard_EmptyDir(t *testing.T) {
	ctx := context.Background()
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	_, err := client.UploadShard(ctx, &runbundle.Shard{Dir: ""})
	if err == nil {
		t.Fatal("expected error for empty Dir")
	}
}

// --- UploadWithRetry tests ---

func TestUploadWithRetry_OutsideGHA(t *testing.T) {
	ctx := context.Background()
	os.Unsetenv("GITHUB_ACTIONS")

	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))
	shardDir := t.TempDir()

	_, err := client.UploadWithRetry(ctx, &runbundle.Shard{
		Dir:    shardDir,
		ExecID: "gh-123-1-abc",
		Role:   runbundle.ShardRolePlan,
		Suffix: "abc123",
		Status: "created",
	})
	if err == nil {
		t.Fatal("expected error when not in GitHub Actions")
	}
	if !strings.Contains(err.Error(), "non-retryable") {
		t.Logf("error: %v", err)
	}
}

func TestUploadWithRetry_NilShard(t *testing.T) {
	ctx := context.Background()
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	_, err := client.UploadWithRetry(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil shard")
	}
}

// --- UploadRunResultArtifact tests ---

func TestUploadRunResultArtifact_DelegatesToUploadShard(t *testing.T) {
	ctx := context.Background()
	os.Unsetenv("GITHUB_ACTIONS")
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	_, err := client.UploadRunResultArtifact(ctx, &runbundle.Shard{Dir: ""})
	if err == nil {
		t.Fatal("expected error for empty Dir")
	}
}

// --- Normalize functions ---

func TestNormalizeZipPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo/bar.txt", "foo/bar.txt"},
		{"/foo/bar.txt", "foo/bar.txt"},
		{"./foo/bar.txt", "foo/bar.txt"},
		{`foo\bar.txt`, "foo/bar.txt"},
		{"/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeZipPath(tt.input)
			if got != tt.want {
				t.Errorf("normalizeZipPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Context cancellation tests ---

func TestRetryDo_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, _ := NewClient(ctx, "owner/repo",
		WithBaseURL(server.URL),
		WithToken("test"),
		WithRetryConfig(RetryConfig{
			MaxRetries:      5,
			InitialDelay:    100 * time.Millisecond,
			MaxDelay:        200 * time.Millisecond,
			JitterFactor:    0,
			RetryableStatus: []int{503},
		}),
	)

	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	req, _ := client.newRequest(ctx, server.URL+"/test")
	_, err := client.doRequest(req)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}