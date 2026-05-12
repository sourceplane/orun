package ci

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// BuildEventContext detects the current CI environment and returns a
// provider-neutral EventContext. Returns nil when no CI is detected.
func BuildEventContext(getenv func(string) string, readFile func(string) ([]byte, error)) *model.EventContext {
	refs := DetectRefs(getenv, readFile)
	if refs.Provider == ProviderNone {
		return nil
	}

	ctx := &model.EventContext{
		Provider: providerToName(refs.Provider),
		Event:    refs.EventType,
		BaseRef:  refs.Base,
		HeadSha:  refs.Head,
	}

	switch refs.Provider {
	case ProviderGitHubActions:
		ctx.Ref = strings.TrimSpace(getenv("GITHUB_REF"))
		ctx.Actor = strings.TrimSpace(getenv("GITHUB_ACTOR"))
		ctx.Repository = strings.TrimSpace(getenv("GITHUB_REPOSITORY"))
		ctx.BaseSha = refs.Base
		ctx.Branch = extractBranch(refs, getenv)
		ctx.Action = extractGitHubAction(getenv("GITHUB_EVENT_PATH"), readFile)

	case ProviderGitLabCI:
		ctx.Ref = strings.TrimSpace(getenv("CI_COMMIT_REF_NAME"))
		ctx.Branch = strings.TrimSpace(getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME"))
		if ctx.Branch == "" {
			ctx.Branch = ctx.Ref
		}
		ctx.Actor = strings.TrimSpace(getenv("GITLAB_USER_LOGIN"))
		ctx.Repository = strings.TrimSpace(getenv("CI_PROJECT_PATH"))

	case ProviderBuildkite:
		ctx.Branch = strings.TrimSpace(getenv("BUILDKITE_BRANCH"))
		ctx.Actor = strings.TrimSpace(getenv("BUILDKITE_BUILD_CREATOR"))
		ctx.Repository = strings.TrimSpace(getenv("BUILDKITE_REPO"))
		ctx.Ref = strings.TrimSpace(getenv("BUILDKITE_BRANCH"))
	}

	return ctx
}

// LoadEventContextFromFile reads a JSON file and unmarshals it into an EventContext.
func LoadEventContextFromFile(path string) (*model.EventContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ctx model.EventContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, err
	}
	return &ctx, nil
}

func providerToName(p Provider) string {
	switch p {
	case ProviderGitHubActions:
		return "github"
	case ProviderGitLabCI:
		return "gitlab"
	case ProviderBuildkite:
		return "buildkite"
	default:
		return string(p)
	}
}

func extractBranch(refs DetectedRefs, getenv func(string) string) string {
	switch refs.EventType {
	case "pull_request", "pull_request_target":
		return strings.TrimSpace(getenv("GITHUB_BASE_REF"))
	case "push":
		ref := strings.TrimSpace(getenv("GITHUB_REF"))
		if strings.HasPrefix(ref, "refs/heads/") {
			return strings.TrimPrefix(ref, "refs/heads/")
		}
		return ref
	default:
		return strings.TrimSpace(getenv("GITHUB_REF"))
	}
}

func extractGitHubAction(eventPath string, readFile func(string) ([]byte, error)) string {
	if eventPath == "" || readFile == nil {
		return ""
	}
	data, err := readFile(strings.TrimSpace(eventPath))
	if err != nil {
		return ""
	}
	var payload struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Action
}
