package ci

import "strings"

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

func DetectRefs(getenv func(string) string) DetectedRefs {
	if strings.EqualFold(strings.TrimSpace(getenv("GITHUB_ACTIONS")), "true") {
		return detectGitHubActions(getenv)
	}
	if strings.TrimSpace(getenv("GITLAB_CI")) != "" {
		return detectGitLabCI(getenv)
	}
	if strings.TrimSpace(getenv("BUILDKITE")) != "" {
		return detectBuildkite(getenv)
	}
	return DetectedRefs{}
}

func detectGitHubActions(getenv func(string) string) DetectedRefs {
	event := strings.TrimSpace(getenv("GITHUB_EVENT_NAME"))
	baseRef := strings.TrimSpace(getenv("GITHUB_BASE_REF"))
	headRef := strings.TrimSpace(getenv("GITHUB_HEAD_REF"))
	sha := strings.TrimSpace(getenv("GITHUB_SHA"))

	envVars := map[string]string{
		"GITHUB_ACTIONS":    getenv("GITHUB_ACTIONS"),
		"GITHUB_EVENT_NAME": event,
		"GITHUB_BASE_REF":   baseRef,
		"GITHUB_HEAD_REF":   headRef,
		"GITHUB_SHA":        sha,
	}

	switch event {
	case "pull_request", "pull_request_target":
		if baseRef != "" && headRef != "" {
			return DetectedRefs{
				Provider:  ProviderGitHubActions,
				EventType: event,
				Base:      baseRef,
				Head:      headRef,
				Reason:    "GitHub Actions " + event + " event",
				EnvVars:   envVars,
			}
		}
		base := baseRef
		if base == "" {
			base = "main"
		}
		head := headRef
		if head == "" {
			head = sha
		}
		return DetectedRefs{
			Provider:  ProviderGitHubActions,
			EventType: event,
			Base:      base,
			Head:      head,
			Reason:    "GitHub Actions " + event + " event (partial env)",
			EnvVars:   envVars,
		}

	case "push":
		return DetectedRefs{
			Provider:  ProviderGitHubActions,
			EventType: "push",
			Base:      "main",
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
