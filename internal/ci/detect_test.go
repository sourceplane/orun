package ci

import "testing"

func TestDetectRefs(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantProv  Provider
		wantEvent string
		wantBase  string
		wantHead  string
	}{
		{
			name:      "no CI environment",
			env:       map[string]string{},
			wantProv:  ProviderNone,
			wantEvent: "",
			wantBase:  "",
			wantHead:  "",
		},
		{
			name: "GitHub Actions pull_request",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "pull_request",
				"GITHUB_BASE_REF":   "main",
				"GITHUB_HEAD_REF":   "feat/auto-detect",
				"GITHUB_SHA":        "abc123",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "pull_request",
			wantBase:  "main",
			wantHead:  "feat/auto-detect",
		},
		{
			name: "GitHub Actions pull_request_target",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "pull_request_target",
				"GITHUB_BASE_REF":   "develop",
				"GITHUB_HEAD_REF":   "fix/bug",
				"GITHUB_SHA":        "def456",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "pull_request_target",
			wantBase:  "develop",
			wantHead:  "fix/bug",
		},
		{
			name: "GitHub Actions push",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "push",
				"GITHUB_SHA":        "abc123def",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "abc123def",
		},
		{
			name: "GitHub Actions workflow_dispatch",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "workflow_dispatch",
				"GITHUB_SHA":        "sha789",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "workflow_dispatch",
			wantBase:  "main",
			wantHead:  "sha789",
		},
		{
			name: "GitHub Actions schedule",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "schedule",
				"GITHUB_SHA":        "sha000",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "schedule",
			wantBase:  "main",
			wantHead:  "sha000",
		},
		{
			name: "GitHub Actions PR with missing head ref falls back to SHA",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "pull_request",
				"GITHUB_BASE_REF":   "main",
				"GITHUB_HEAD_REF":   "",
				"GITHUB_SHA":        "fallback123",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "pull_request",
			wantBase:  "main",
			wantHead:  "fallback123",
		},
		{
			name: "GitHub Actions with no event name",
			env: map[string]string{
				"GITHUB_ACTIONS": "true",
				"GITHUB_SHA":     "noev123",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "unknown",
			wantBase:  "main",
			wantHead:  "noev123",
		},
		{
			name: "GitLab CI merge request",
			env: map[string]string{
				"GITLAB_CI":                           "true",
				"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main",
				"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feat/gitlab",
				"CI_COMMIT_SHA":                       "gl123",
			},
			wantProv:  ProviderGitLabCI,
			wantEvent: "merge_request",
			wantBase:  "main",
			wantHead:  "feat/gitlab",
		},
		{
			name: "GitLab CI merge request with missing source branch",
			env: map[string]string{
				"GITLAB_CI":                           "true",
				"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "develop",
				"CI_COMMIT_SHA":                       "gl456",
			},
			wantProv:  ProviderGitLabCI,
			wantEvent: "merge_request",
			wantBase:  "develop",
			wantHead:  "gl456",
		},
		{
			name: "GitLab CI branch pipeline",
			env: map[string]string{
				"GITLAB_CI":    "true",
				"CI_COMMIT_SHA": "glbranch789",
			},
			wantProv:  ProviderGitLabCI,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "glbranch789",
		},
		{
			name: "GitLab CI branch pipeline no SHA",
			env: map[string]string{
				"GITLAB_CI": "true",
			},
			wantProv:  ProviderGitLabCI,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "HEAD",
		},
		{
			name: "Buildkite pull request",
			env: map[string]string{
				"BUILDKITE":                          "true",
				"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main",
				"BUILDKITE_COMMIT":                   "bk123",
			},
			wantProv:  ProviderBuildkite,
			wantEvent: "pull_request",
			wantBase:  "main",
			wantHead:  "bk123",
		},
		{
			name: "Buildkite push",
			env: map[string]string{
				"BUILDKITE":       "true",
				"BUILDKITE_COMMIT": "bkpush456",
			},
			wantProv:  ProviderBuildkite,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "bkpush456",
		},
		{
			name: "Buildkite push no commit",
			env: map[string]string{
				"BUILDKITE": "true",
			},
			wantProv:  ProviderBuildkite,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "HEAD",
		},
		{
			name: "GitHub Actions takes precedence over GitLab",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "push",
				"GITHUB_SHA":        "ghwin",
				"GITLAB_CI":         "true",
				"CI_COMMIT_SHA":     "gllose",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "ghwin",
		},
		{
			name: "GITHUB_ACTIONS case insensitive",
			env: map[string]string{
				"GITHUB_ACTIONS":    "True",
				"GITHUB_EVENT_NAME": "push",
				"GITHUB_SHA":        "ci123",
			},
			wantProv:  ProviderGitHubActions,
			wantEvent: "push",
			wantBase:  "main",
			wantHead:  "ci123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			got := DetectRefs(getenv)

			if got.Provider != tt.wantProv {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.wantProv)
			}
			if got.EventType != tt.wantEvent {
				t.Errorf("EventType = %q, want %q", got.EventType, tt.wantEvent)
			}
			if got.Base != tt.wantBase {
				t.Errorf("Base = %q, want %q", got.Base, tt.wantBase)
			}
			if got.Head != tt.wantHead {
				t.Errorf("Head = %q, want %q", got.Head, tt.wantHead)
			}
		})
	}
}

func TestDetectedRefs_EnvVarsPopulated(t *testing.T) {
	getenv := func(key string) string {
		m := map[string]string{
			"GITHUB_ACTIONS":    "true",
			"GITHUB_EVENT_NAME": "pull_request",
			"GITHUB_BASE_REF":   "main",
			"GITHUB_HEAD_REF":   "feat/x",
			"GITHUB_SHA":        "sha1",
		}
		return m[key]
	}

	got := DetectRefs(getenv)
	if got.EnvVars == nil {
		t.Fatal("EnvVars should not be nil")
	}
	if got.EnvVars["GITHUB_EVENT_NAME"] != "pull_request" {
		t.Errorf("EnvVars[GITHUB_EVENT_NAME] = %q, want %q", got.EnvVars["GITHUB_EVENT_NAME"], "pull_request")
	}
	if got.EnvVars["GITHUB_BASE_REF"] != "main" {
		t.Errorf("EnvVars[GITHUB_BASE_REF] = %q, want %q", got.EnvVars["GITHUB_BASE_REF"], "main")
	}
}

func TestDetectedRefs_ReasonNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"github pr", map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request", "GITHUB_BASE_REF": "main", "GITHUB_HEAD_REF": "f"}},
		{"github push", map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "push", "GITHUB_SHA": "x"}},
		{"gitlab mr", map[string]string{"GITLAB_CI": "true", "CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main", "CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "f"}},
		{"gitlab push", map[string]string{"GITLAB_CI": "true", "CI_COMMIT_SHA": "x"}},
		{"buildkite pr", map[string]string{"BUILDKITE": "true", "BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main", "BUILDKITE_COMMIT": "x"}},
		{"buildkite push", map[string]string{"BUILDKITE": "true", "BUILDKITE_COMMIT": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			got := DetectRefs(getenv)
			if got.Reason == "" {
				t.Error("Reason should not be empty for detected CI provider")
			}
		})
	}
}
