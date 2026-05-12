package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildEventContext_GitHubPR(t *testing.T) {
	eventPayload := `{"action":"synchronize"}`
	eventFile := writeTestFile(t, eventPayload)

	env := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_BASE_REF":   "main",
		"GITHUB_HEAD_REF":   "feature/x",
		"GITHUB_SHA":        "abc123",
		"GITHUB_REF":        "refs/pull/42/merge",
		"GITHUB_ACTOR":      "dev",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_EVENT_PATH": eventFile,
	}

	ctx := BuildEventContext(mockGetenv(env), os.ReadFile)
	if ctx == nil {
		t.Fatal("expected non-nil EventContext")
	}
	if ctx.Provider != "github" {
		t.Fatalf("provider = %q, want github", ctx.Provider)
	}
	if ctx.Event != "pull_request" {
		t.Fatalf("event = %q, want pull_request", ctx.Event)
	}
	if ctx.Action != "synchronize" {
		t.Fatalf("action = %q, want synchronize", ctx.Action)
	}
	if ctx.Branch != "main" {
		t.Fatalf("branch = %q, want main", ctx.Branch)
	}
	if ctx.HeadSha != "abc123" {
		t.Fatalf("headSha = %q, want abc123", ctx.HeadSha)
	}
	if ctx.Actor != "dev" {
		t.Fatalf("actor = %q, want dev", ctx.Actor)
	}
	if ctx.Repository != "org/repo" {
		t.Fatalf("repository = %q, want org/repo", ctx.Repository)
	}
}

func TestBuildEventContext_GitHubPush(t *testing.T) {
	eventPayload := `{"before":"def456"}`
	eventFile := writeTestFile(t, eventPayload)

	env := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_SHA":        "abc123",
		"GITHUB_REF":        "refs/heads/main",
		"GITHUB_ACTOR":      "dev",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_EVENT_PATH": eventFile,
	}

	ctx := BuildEventContext(mockGetenv(env), os.ReadFile)
	if ctx == nil {
		t.Fatal("expected non-nil EventContext")
	}
	if ctx.Event != "push" {
		t.Fatalf("event = %q, want push", ctx.Event)
	}
	if ctx.Branch != "main" {
		t.Fatalf("branch = %q, want main", ctx.Branch)
	}
}

func TestBuildEventContext_GitLabMR(t *testing.T) {
	env := map[string]string{
		"GITLAB_CI":                           "true",
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main",
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feature/x",
		"CI_COMMIT_SHA":                       "abc123",
		"CI_COMMIT_REF_NAME":                  "feature/x",
		"GITLAB_USER_LOGIN":                   "dev",
		"CI_PROJECT_PATH":                     "group/project",
	}

	ctx := BuildEventContext(mockGetenv(env), nil)
	if ctx == nil {
		t.Fatal("expected non-nil EventContext")
	}
	if ctx.Provider != "gitlab" {
		t.Fatalf("provider = %q, want gitlab", ctx.Provider)
	}
	if ctx.Event != "merge_request" {
		t.Fatalf("event = %q, want merge_request", ctx.Event)
	}
	if ctx.Branch != "main" {
		t.Fatalf("branch = %q, want main", ctx.Branch)
	}
}

func TestBuildEventContext_NoCI(t *testing.T) {
	ctx := BuildEventContext(mockGetenv(nil), nil)
	if ctx != nil {
		t.Fatal("expected nil EventContext for non-CI environment")
	}
}

func TestLoadEventContextFromFile(t *testing.T) {
	payload := `{"provider":"github","event":"pull_request","action":"opened","branch":"main","headSha":"abc123"}`
	path := writeTestFile(t, payload)

	ctx, err := LoadEventContextFromFile(path)
	if err != nil {
		t.Fatalf("LoadEventContextFromFile: %v", err)
	}
	if ctx.Provider != "github" {
		t.Fatalf("provider = %q, want github", ctx.Provider)
	}
	if ctx.Event != "pull_request" {
		t.Fatalf("event = %q, want pull_request", ctx.Event)
	}
	if ctx.Action != "opened" {
		t.Fatalf("action = %q, want opened", ctx.Action)
	}
	if ctx.Branch != "main" {
		t.Fatalf("branch = %q, want main", ctx.Branch)
	}
}

func TestLoadEventContextFromFile_NotFound(t *testing.T) {
	_, err := LoadEventContextFromFile("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadEventContextFromFile_InvalidJSON(t *testing.T) {
	path := writeTestFile(t, "not json")
	_, err := LoadEventContextFromFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func mockGetenv(env map[string]string) func(string) string {
	return func(key string) string {
		if env == nil {
			return ""
		}
		return env[key]
	}
}

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "event.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildEventContext_GitHubTag(t *testing.T) {
	eventPayload := `{}`
	eventFile := writeTestFile(t, eventPayload)

	env := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_SHA":        "abc123",
		"GITHUB_REF":        "refs/tags/v1.0.0",
		"GITHUB_EVENT_PATH": eventFile,
	}

	ctx := BuildEventContext(mockGetenv(env), os.ReadFile)
	if ctx == nil {
		t.Fatal("expected non-nil EventContext")
	}
	if ctx.Ref != "refs/tags/v1.0.0" {
		t.Fatalf("ref = %q, want refs/tags/v1.0.0", ctx.Ref)
	}
}

func TestProviderToName(t *testing.T) {
	cases := []struct {
		p    Provider
		want string
	}{
		{ProviderGitHubActions, "github"},
		{ProviderGitLabCI, "gitlab"},
		{ProviderBuildkite, "buildkite"},
		{ProviderNone, ""},
	}
	for _, tc := range cases {
		got := providerToName(tc.p)
		if got != tc.want {
			t.Errorf("providerToName(%q) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestExtractGitHubAction_Valid(t *testing.T) {
	payload := map[string]string{"action": "opened"}
	data, _ := json.Marshal(payload)
	path := writeTestFile(t, string(data))

	action := extractGitHubAction(path, os.ReadFile)
	if action != "opened" {
		t.Fatalf("action = %q, want opened", action)
	}
}

func TestExtractGitHubAction_NoFile(t *testing.T) {
	action := extractGitHubAction("", nil)
	if action != "" {
		t.Fatalf("expected empty, got %q", action)
	}
}
