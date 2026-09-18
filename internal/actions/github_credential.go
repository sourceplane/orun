package actions

import (
	"context"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/agent/ground"
	"github.com/sourceplane/orun/internal/cliauth"
)

// THE BUILD'S GITHUB CREDENTIAL, wherever it is running.
//
// On a laptop it is the operator's: GITHUB_TOKEN / GH_TOKEN / `gh auth`. In a
// platform sandbox there is none of that — the first console build to reach
// phase 01's `repo` hook died on
//
//	✕ hook "repo" (orun.repo/ensure@v1): a GitHub credential is required
//	  (GITHUB_TOKEN / GH_TOKEN / gh auth)
//
// — but the sandbox is a session grounded on the product's repository, and the
// platform mints a token for exactly that repository on request. It is what
// `orun git-credential` already hands git there. So the environment is asked
// first, and the platform second.
//
// A FUNCTION, not a value: minted tokens last an hour and a landing's check
// wait or a convergence watch can last as long, so each request asks again
// (the platform side caches, so asking is a file read until the token is
// near expiry).

// platformGitHubToken is the platform's repo token for this session. A seam
// for tests; the real one reads the grounded session from the environment.
var platformGitHubToken = func(ctx context.Context) (string, error) {
	return ground.RepoTokenFromEnv(ctx, os.Getenv)
}

// githubToken is the credential for one GitHub request, or "" when there is
// none anywhere.
func githubToken(ctx context.Context) string {
	if t := strings.TrimSpace(cliauth.GitHubTokenFromEnv()); t != "" {
		return t
	}
	if t, err := platformGitHubToken(ctx); err == nil {
		return strings.TrimSpace(t)
	}
	return ""
}

// inPlatformSession reports whether this process runs in a platform sandbox
// grounded on a repository — the case where git reaches GitHub through
// `orun git-credential` rather than through anything the operator set up.
var inPlatformSession = func() bool {
	s, err := ground.SessionFromEnv(os.Getenv)
	return err == nil && s.FullName != ""
}
