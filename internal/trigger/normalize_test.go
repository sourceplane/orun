package trigger

import (
	"encoding/json"
	"testing"
)

func TestNormalizeGitHubEvent_PullRequest(t *testing.T) {
	raw := map[string]any{
		"action": "synchronize",
		"pull_request": map[string]any{
			"base": map[string]any{
				"ref": "main",
				"sha": "abc123",
			},
			"head": map[string]any{
				"ref": "feature/foo",
				"sha": "def456",
			},
		},
		"repository": map[string]any{
			"full_name": "org/repo",
		},
		"sender": map[string]any{
			"login": "user1",
		},
	}

	ev := NormalizeGitHubEvent("pull_request", raw)

	assertEqual(t, "provider", "github", ev.Provider)
	assertEqual(t, "event", "pull_request", ev.Event)
	assertEqual(t, "action", "synchronize", ev.Action)
	assertEqual(t, "refType", "pull_request", ev.RefType)
	assertEqual(t, "baseBranch", "main", ev.BaseBranch)
	assertEqual(t, "branch", "feature/foo", ev.Branch)
	assertEqual(t, "baseSHA", "abc123", ev.BaseSHA)
	assertEqual(t, "headSHA", "def456", ev.HeadSHA)
	assertEqual(t, "repository", "org/repo", ev.Repository)
	assertEqual(t, "actor", "user1", ev.Actor)
}

func TestNormalizeGitHubEvent_PushBranch(t *testing.T) {
	raw := map[string]any{
		"ref":    "refs/heads/main",
		"before": "aaa111",
		"after":  "bbb222",
		"repository": map[string]any{
			"full_name": "org/repo",
		},
		"sender": map[string]any{
			"login": "deployer",
		},
	}

	ev := NormalizeGitHubEvent("push", raw)

	assertEqual(t, "provider", "github", ev.Provider)
	assertEqual(t, "event", "push", ev.Event)
	assertEqual(t, "ref", "refs/heads/main", ev.Ref)
	assertEqual(t, "refType", "branch", ev.RefType)
	assertEqual(t, "branch", "main", ev.Branch)
	assertEqual(t, "tag", "", ev.Tag)
	assertEqual(t, "baseSHA", "aaa111", ev.BaseSHA)
	assertEqual(t, "headSHA", "bbb222", ev.HeadSHA)
}

func TestNormalizeGitHubEvent_PushTag(t *testing.T) {
	raw := map[string]any{
		"ref":    "refs/tags/v1.2.0",
		"before": "000000",
		"after":  "ccc333",
		"repository": map[string]any{
			"full_name": "org/repo",
		},
		"sender": map[string]any{
			"login": "releaser",
		},
	}

	ev := NormalizeGitHubEvent("push", raw)

	assertEqual(t, "refType", "tag", ev.RefType)
	assertEqual(t, "tag", "v1.2.0", ev.Tag)
	assertEqual(t, "branch", "", ev.Branch)
}

func TestNormalizeGitHubEvent_WorkflowDispatch(t *testing.T) {
	raw := map[string]any{
		"ref": "refs/heads/main",
		"repository": map[string]any{
			"full_name": "org/repo",
		},
		"sender": map[string]any{
			"login": "operator",
		},
		"workflow": ".github/workflows/deploy.yml",
	}

	ev := NormalizeGitHubEvent("workflow_dispatch", raw)

	assertEqual(t, "event", "workflow_dispatch", ev.Event)
	assertEqual(t, "refType", "branch", ev.RefType)
	assertEqual(t, "branch", "main", ev.Branch)
}

func TestParseEventFile_GitHub(t *testing.T) {
	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"base": map[string]any{"ref": "main", "sha": "base1"},
			"head": map[string]any{"ref": "feat", "sha": "head1"},
		},
		"repository": map[string]any{"full_name": "org/repo"},
		"sender":     map[string]any{"login": "dev"},
	}
	data, _ := json.Marshal(payload)

	ev, err := ParseEventFile("github", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "event", "pull_request", ev.Event)
	assertEqual(t, "action", "opened", ev.Action)
}

func TestParseEventFileWithName(t *testing.T) {
	payload := map[string]any{
		"ref":    "refs/heads/main",
		"before": "aaa",
		"after":  "bbb",
		"repository": map[string]any{"full_name": "org/repo"},
		"sender":     map[string]any{"login": "ci"},
	}
	data, _ := json.Marshal(payload)

	ev, err := ParseEventFileWithName("github", "push", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "event", "push", ev.Event)
	assertEqual(t, "branch", "main", ev.Branch)
}

func TestParseEventFile_UnsupportedProvider(t *testing.T) {
	_, err := ParseEventFile("gitlab", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if actual != expected {
		t.Errorf("%s: expected %q, got %q", field, expected, actual)
	}
}
