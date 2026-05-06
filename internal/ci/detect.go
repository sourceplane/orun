package ci

import (
	"encoding/json"
	"strings"
)

type Provider string

const (
	ProviderNone          Provider = ""
	ProviderGitHubActions Provider = "github-actions"
	ProviderGitLabCI      Provider = "gitlab-ci"
	ProviderBuildkite     Provider = "buildkite"
)

type DetectedRefs struct {
	Provider  Provider
	EventType string
	Base      string
	Head      string
	Reason    string
	EnvVars   map[string]string
}

func DetectRefs(getenv func(string) string, readFile func(string) ([]byte, error)) DetectedRefs {
	if strings.EqualFold(strings.TrimSpace(getenv("GITHUB_ACTIONS")), "true") {
		return detectGitHubActions(getenv, readFile)
	}
	if strings.TrimSpace(getenv("GITLAB_CI")) != "" {
		return detectGitLabCI(getenv)
	}
	if strings.TrimSpace(getenv("BUILDKITE")) != "" {
		return detectBuildkite(getenv)
	}
	return DetectedRefs{}
}

func detectGitHubActions(getenv func(string) string, readFile func(string) ([]byte, error)) DetectedRefs {
	event := strings.TrimSpace(getenv("GITHUB_EVENT_NAME"))
	baseRef := strings.TrimSpace(getenv("GITHUB_BASE_REF"))
	headRef := strings.TrimSpace(getenv("GITHUB_HEAD_REF"))
	sha := strings.TrimSpace(getenv("GITHUB_SHA"))

	envVars := map[string]string{
		"GITHUB_ACTIONS":     getenv("GITHUB_ACTIONS"),
		"GITHUB_EVENT_NAME":  event,
		"GITHUB_BASE_REF":    baseRef,
		"GITHUB_HEAD_REF":    headRef,
		"GITHUB_SHA":         sha,
		"GITHUB_EVENT_PATH":  getenv("GITHUB_EVENT_PATH"),
	}

	switch event {
	case "pull_request", "pull_request_target":
		base := baseRef
		if base == "" {
			base = "main"
		}
		// Use GITHUB_SHA (the merge commit) not GITHUB_HEAD_REF (branch name).
		// actions/checkout checks out the merge commit as a detached HEAD; the PR
		// branch name in GITHUB_HEAD_REF has no local ref and git diff/merge-base
		// fail silently, returning zero changed files.
		head := sha
		if head == "" {
			head = headRef
		}
		reason := "GitHub Actions " + event + " event"
		if baseRef == "" || sha == "" {
			reason += " (partial env)"
		}
		return DetectedRefs{
			Provider:  ProviderGitHubActions,
			EventType: event,
			Base:      base,
			Head:      head,
			Reason:    reason,
			EnvVars:   envVars,
		}

	case "push":
		base := pushEventBase(getenv("GITHUB_EVENT_PATH"), readFile)
		return DetectedRefs{
			Provider:  ProviderGitHubActions,
			EventType: "push",
			Base:      base,
			Head:      sha,
			Reason:    "GitHub Actions push event",
			EnvVars:   envVars,
		}

	default:
		if event == "" {
			event = "unknown"
		}
		head := sha
		if head == "" {
			head = "HEAD"
		}
		return DetectedRefs{
			Provider:  ProviderGitHubActions,
			EventType: event,
			Base:      "main",
			Head:      head,
			Reason:    "GitHub Actions " + event + " event",
			EnvVars:   envVars,
		}
	}
}

func detectGitLabCI(getenv func(string) string) DetectedRefs {
	targetBranch := strings.TrimSpace(getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME"))
	sourceBranch := strings.TrimSpace(getenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"))
	commitSHA := strings.TrimSpace(getenv("CI_COMMIT_SHA"))

	envVars := map[string]string{
		"GITLAB_CI":                             getenv("GITLAB_CI"),
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME":   targetBranch,
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME":   sourceBranch,
		"CI_COMMIT_SHA":                         commitSHA,
	}

	if targetBranch != "" {
		head := sourceBranch
		if head == "" {
			head = commitSHA
		}
		return DetectedRefs{
			Provider:  ProviderGitLabCI,
			EventType: "merge_request",
			Base:      targetBranch,
			Head:      head,
			Reason:    "GitLab CI merge request pipeline",
			EnvVars:   envVars,
		}
	}

	head := commitSHA
	if head == "" {
		head = "HEAD"
	}
	return DetectedRefs{
		Provider:  ProviderGitLabCI,
		EventType: "push",
		Base:      "main",
		Head:      head,
		Reason:    "GitLab CI branch pipeline",
		EnvVars:   envVars,
	}
}

func detectBuildkite(getenv func(string) string) DetectedRefs {
	baseBranch := strings.TrimSpace(getenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH"))
	commit := strings.TrimSpace(getenv("BUILDKITE_COMMIT"))

	envVars := map[string]string{
		"BUILDKITE":                          getenv("BUILDKITE"),
		"BUILDKITE_PULL_REQUEST_BASE_BRANCH": baseBranch,
		"BUILDKITE_COMMIT":                   commit,
	}

	head := commit
	if head == "" {
		head = "HEAD"
	}

	if baseBranch != "" {
		return DetectedRefs{
			Provider:  ProviderBuildkite,
			EventType: "pull_request",
			Base:      baseBranch,
			Head:      head,
			Reason:    "Buildkite pull request build",
			EnvVars:   envVars,
		}
	}

	return DetectedRefs{
		Provider:  ProviderBuildkite,
		EventType: "push",
		Base:      "main",
		Head:      head,
		Reason:    "Buildkite push build",
		EnvVars:   envVars,
	}
}

// pushEventBase reads the GitHub push event payload to get the "before" SHA,
// which is main's HEAD before the merge. This is the correct base for diffing
// what changed in a merge — using GITHUB_SHA (after) as both base and head
// produces an empty diff because the merge commit IS now the head of main.
func pushEventBase(eventPath string, readFile func(string) ([]byte, error)) string {
	if eventPath == "" || readFile == nil {
		return "main"
	}
	data, err := readFile(eventPath)
	if err != nil {
		return "main"
	}
	var payload struct {
		Before string `json:"before"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "main"
	}
	// All-zero SHA means the branch was just created; fall back to "main".
	before := strings.TrimSpace(payload.Before)
	if before == "" || isZeroSHA(before) {
		return "main"
	}
	return before
}

func isZeroSHA(sha string) bool {
	if len(sha) < 40 {
		return false
	}
	for _, c := range sha {
		if c != '0' {
			return false
		}
	}
	return true
}
