package actions

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// stubPlatform stands in for the platform's repo-token door and for "this
// process is a grounded sandbox session".
func stubPlatform(t *testing.T, token string, err error, inSession bool) *int {
	t.Helper()
	calls := 0
	prevTok, prevIn := platformGitHubToken, inPlatformSession
	platformGitHubToken = func(context.Context) (string, error) { calls++; return token, err }
	inPlatformSession = func() bool { return inSession }
	t.Cleanup(func() { platformGitHubToken, inPlatformSession = prevTok, prevIn })
	return &calls
}

// THE CONSOLE BUILD THAT HAD NO CREDENTIAL. A platform sandbox carries no
// GITHUB_TOKEN and no `gh` login, and phase 01's `repo` hook died asking for
// one — in a session grounded on the very repository it wanted.
func TestAGroundedSandboxUsesThePlatformsRepoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // no `gh` to ask
	stubPlatform(t, "ghs_platform", nil, true)
	if got := githubToken(context.Background()); got != "ghs_platform" {
		t.Fatalf("githubToken = %q, want the platform's", got)
	}
	c, err := forgeClient(context.Background())
	if err != nil {
		t.Fatalf("forgeClient in a grounded sandbox: %v", err)
	}
	if c.TokenFn == nil {
		t.Fatal("the client must ask per request: a watch outlives a minted token")
	}
}

// The operator's own credential wins where there is one: a laptop run is
// unchanged, and the platform is not asked.
func TestTheEnvironmentsTokenWinsOverThePlatforms(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_operator")
	calls := stubPlatform(t, "ghs_platform", nil, true)
	if got := githubToken(context.Background()); got != "ghp_operator" {
		t.Fatalf("githubToken = %q, want the environment's", got)
	}
	if *calls != 0 {
		t.Errorf("asked the platform %d time(s) with a token already in hand", *calls)
	}
}

func TestNoCredentialAnywhereIsStillRefusedByName(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	stubPlatform(t, "", errors.New("not a grounded session"), false)
	_, err := forgeClient(context.Background())
	if err == nil || !strings.Contains(err.Error(), "a GitHub credential is required") {
		t.Fatalf("want the named refusal, got %v", err)
	}
}

// git in the sandbox reaches GitHub over https through `orun git-credential`.
// The product repository a build creates is not the grounded clone that
// carries that helper, so wiring origin must give it one — repo-local, and a
// command, never a credential.
func TestRepoEnsureInASandboxWiresHTTPSAndTheOrunHelper(t *testing.T) {
	isolateGit(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	stubPlatform(t, "ghs_platform", nil, true)
	product := t.TempDir()
	gitT(t, product, "init", "-q")
	url, err := wireOrigin(context.Background(), product, "orunbase-demo", "newne")
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	if url != "https://github.com/orunbase-demo/newne.git" {
		t.Fatalf("origin = %q, want https (a sandbox has no SSH key)", url)
	}
	helper := gitT(t, product, "config", "--local", "--get", "credential.https://github.com.helper")
	if !strings.HasPrefix(helper, "!") || !strings.HasSuffix(helper, " git-credential") {
		t.Fatalf("credential helper = %q, want `!<orun> git-credential`", helper)
	}
	if strings.Contains(helper, "ghs_") {
		t.Fatal("a credential was written into git config")
	}
}

// Off the platform nothing about git changes.
func TestRepoEnsureOffThePlatformLeavesGitsHelpersAlone(t *testing.T) {
	isolateGit(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	stubPlatform(t, "", errors.New("not a grounded session"), false)
	product := t.TempDir()
	gitT(t, product, "init", "-q")
	if _, err := wireOrigin(context.Background(), product, "sourceplane", "altocumulus"); err != nil {
		t.Fatal(err)
	}
	// `--get-regexp` exits 1 when nothing matches — which is the answer wanted.
	out, _ := exec.Command("git", "-C", product, "config", "--local", "--get-regexp", "credential").CombinedOutput()
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Fatalf("configured a credential helper off the platform: %q", got)
	}
}

// The landing writes with the same credential, and asks each time.
func TestTheLandingWritesWithThePlatformsToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	calls := stubPlatform(t, "ghs_platform", nil, true)
	pen := landPen(context.Background(), t.TempDir())
	if pen.Token == nil || pen.Token() != "ghs_platform" || pen.Token() != "ghs_platform" {
		t.Fatal("the landing's pen must carry the platform's token")
	}
	if *calls != 2 {
		t.Fatalf("asked the platform %d time(s) for two requests, want 2", *calls)
	}
}
