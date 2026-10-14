package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/githubretry"
)

func client(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{Token: "t0ken", APIBase: srv.URL}
}

func TestEnsureRepoFindsAnExistingOne(t *testing.T) {
	var created bool
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			created = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name": "acme/product", "clone_url": "https://github.com/acme/product.git",
		})
	})
	res, err := c.EnsureRepo(context.Background(), "acme", "product", true, true)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Created {
		t.Error("an existing repo must not be reported as created")
	}
	if created {
		t.Error("nothing should have been POSTed")
	}
}

// fakeGitHub is the slice of the GitHub API EnsureRepo talks to: repositories
// by full name, accounts by login with their kind, and the identity the token
// authenticates as ("" answers /user the way an installation token does).
type fakeGitHub struct {
	repos    map[string]bool
	accounts map[string]string
	me       string
	posts    []fakePost
}

type fakePost struct {
	Path    string
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

func (f *fakeGitHub) client(t *testing.T) *Client {
	t.Helper()
	return client(t, func(w http.ResponseWriter, r *http.Request) {
		notFound := func() {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"):
			full := strings.TrimPrefix(r.URL.Path, "/repos/")
			if !f.repos[full] {
				notFound()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"full_name": full, "clone_url": "https://github.com/" + full + ".git"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/"):
			login := strings.TrimPrefix(r.URL.Path, "/users/")
			kind, ok := f.accounts[login]
			if !ok {
				notFound()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"login": login, "type": kind})
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			if f.me == "" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Resource not accessible by integration"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"login": f.me})
		case r.Method == http.MethodPost:
			post := fakePost{Path: r.URL.Path}
			_ = json.NewDecoder(r.Body).Decode(&post)
			f.posts = append(f.posts, post)
			var owner string
			switch {
			case r.URL.Path == "/user/repos":
				owner = f.me
			case strings.HasPrefix(r.URL.Path, "/orgs/") && f.accounts[strings.Split(r.URL.Path, "/")[2]] == "Organization":
				owner = strings.Split(r.URL.Path, "/")[2]
			default:
				// What GitHub does with /orgs/{user}/repos: the 2026-10-14 failure.
				notFound()
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"full_name": owner + "/" + post.Name, "clone_url": "https://github.com/" + owner + "/" + post.Name + ".git",
			})
		default:
			notFound()
		}
	})
}

// Phase 01 is re-run like any other phase, so the second run must find the
// repository the first one made rather than fail on a name collision.
func TestEnsureRepoCreatesWhenAbsent(t *testing.T) {
	gh := &fakeGitHub{accounts: map[string]string{"acme": "Organization", "bot": "User"}, me: "bot"}
	res, err := gh.client(t).EnsureRepo(context.Background(), "acme", "product", true, true)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !res.Created || res.FullName != "acme/product" {
		t.Errorf("a fresh org repo should be reported as created as acme/product, got %+v", res)
	}
	if len(gh.posts) != 1 || gh.posts[0].Path != "/orgs/acme/repos" {
		t.Fatalf("an org create must use the org endpoint, got %+v", gh.posts)
	}
	if !gh.posts[0].Private {
		t.Error("private must be passed through")
	}
}

// A blueprint's `githuborg` may name a personal account. GitHub answers
// POST /orgs/{user}/repos with a bare 404, which is how
// `--set githuborg=pullely` failed in 01-scaffold; the user's own token
// creates through /user/repos, whatever inOrg says.
func TestEnsureRepoCreatesInTheTokensOwnPersonalAccount(t *testing.T) {
	for _, private := range []bool{true, false} {
		gh := &fakeGitHub{accounts: map[string]string{"pullely": "User"}, me: "Pullely"}
		res, err := gh.client(t).EnsureRepo(context.Background(), "pullely", "product", private, true)
		if err != nil {
			t.Fatalf("private=%v: ensure: %v", private, err)
		}
		if !res.Created || !strings.EqualFold(res.FullName, "pullely/product") {
			t.Errorf("private=%v: want pullely/product created, got %+v", private, res)
		}
		if len(gh.posts) != 1 || gh.posts[0].Path != "/user/repos" {
			t.Fatalf("private=%v: a personal create must use /user/repos, got %+v", private, gh.posts)
		}
		if gh.posts[0].Private != private || gh.posts[0].Name != "product" {
			t.Errorf("private=%v: posted %+v", private, gh.posts[0])
		}
	}
}

// /user/repos creates in the TOKEN's account, so another user's token would
// put the repository somewhere the caller did not ask. Refuse, and say how.
func TestEnsureRepoRefusesAnotherUsersPersonalAccount(t *testing.T) {
	gh := &fakeGitHub{accounts: map[string]string{"pullely": "User", "rahul": "User"}, me: "rahul"}
	_, err := gh.client(t).EnsureRepo(context.Background(), "pullely", "product", false, true)
	if err == nil {
		t.Fatal("creating in someone else's personal account must be refused")
	}
	if len(gh.posts) != 0 {
		t.Errorf("nothing should have been POSTed, got %+v", gh.posts)
	}
	for _, want := range []string{"pullely is a personal GitHub account", "rahul's", "gh repo create pullely/product --public"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got %v", want, err)
		}
	}
}

// An installation token has no user behind it: /user answers 403, and there
// is no personal account for /user/repos to create in.
func TestEnsureRepoRefusesAPersonalAccountWithoutAUserToken(t *testing.T) {
	gh := &fakeGitHub{accounts: map[string]string{"pullely": "User"}}
	_, err := gh.client(t).EnsureRepo(context.Background(), "pullely", "product", true, true)
	if err == nil {
		t.Fatal("a token with no user cannot create in a personal account")
	}
	if len(gh.posts) != 0 {
		t.Errorf("nothing should have been POSTed, got %+v", gh.posts)
	}
	for _, want := range []string{"has no user of its own", "gh repo create pullely/product --private"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got %v", want, err)
		}
	}
}

// The workaround that unblocked 2026-10-14 — pre-creating the repository —
// keeps working: an existing repo is found before the owner is looked at.
func TestEnsureRepoFindsAnExistingRepoInAnotherUsersAccount(t *testing.T) {
	gh := &fakeGitHub{repos: map[string]bool{"pullely/product": true}, accounts: map[string]string{"pullely": "User"}, me: "rahul"}
	res, err := gh.client(t).EnsureRepo(context.Background(), "pullely", "product", true, true)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Created || res.FullName != "pullely/product" {
		t.Errorf("an existing repo must be found, not created; got %+v", res)
	}
	if len(gh.posts) != 0 {
		t.Errorf("nothing should have been POSTed, got %+v", gh.posts)
	}
}

func TestEnsureRepoNamesAnOwnerGitHubDoesNotKnow(t *testing.T) {
	gh := &fakeGitHub{me: "rahul"}
	_, err := gh.client(t).EnsureRepo(context.Background(), "nobody", "product", true, true)
	if err == nil || !strings.Contains(err.Error(), `no organization or user named "nobody"`) {
		t.Fatalf("an unknown owner should be named; got %v", err)
	}
	if len(gh.posts) != 0 {
		t.Errorf("nothing should have been POSTed, got %+v", gh.posts)
	}
}

// When GitHub cannot say what the owner is, the declared inOrg decides, as it
// did before the owner was looked up.
func TestEnsureRepoFallsBackToInOrgWhenTheOwnerCannotBeRead(t *testing.T) {
	for _, tc := range []struct {
		inOrg bool
		want  string
	}{{true, "/orgs/me/repos"}, {false, "/user/repos"}} {
		var postedTo string
		c := client(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost:
				postedTo = r.URL.Path
				_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "me/product"})
			case strings.HasPrefix(r.URL.Path, "/users/"):
				w.WriteHeader(http.StatusUnprocessableEntity)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})
		if _, err := c.EnsureRepo(context.Background(), "me", "product", true, tc.inOrg); err != nil {
			t.Fatalf("inOrg=%v: ensure: %v", tc.inOrg, err)
		}
		if postedTo != tc.want {
			t.Errorf("inOrg=%v: want %s, got %q", tc.inOrg, tc.want, postedTo)
		}
	}
}

// An anonymous 404 is indistinguishable from "absent", so a missing credential
// is refused rather than attempted.
func TestEnsureRepoRefusesWithoutACredential(t *testing.T) {
	c := &Client{}
	if _, err := c.EnsureRepo(context.Background(), "acme", "product", true, true); err == nil {
		t.Fatal("an anonymous ensure must be refused")
	}
}

func TestEnsureRepoSurfacesANonNotFoundError(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "Resource not accessible"})
	})
	_, err := c.EnsureRepo(context.Background(), "acme", "product", true, true)
	if err == nil {
		t.Fatal("a 403 is not 'absent' and must not lead to a create attempt")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("GitHub's own message should come through; got %v", err)
	}
}

func TestLatestRunWithNoRunsReturnsNil(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{}})
	})
	run, err := c.LatestRun(context.Background(), "acme", "product", "main")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if run != nil {
		t.Error("no runs should be nil, not an empty run")
	}
}

func TestFailedJobsNamesOnlyTheFailures(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []map[string]any{
			{"name": "test", "conclusion": "success"},
			{"name": "lint", "conclusion": "failure", "html_url": "https://github.com/acme/product/actions/runs/1/job/11",
				"steps": []map[string]any{
					{"name": "Set up job", "conclusion": "success", "number": 1},
					{"name": "Run eslint", "conclusion": "failure", "number": 4},
					{"name": "Post checkout", "conclusion": "skipped", "number": 9},
				}},
			{"name": "matrix", "conclusion": "skipped"},
			{"name": "codeql", "conclusion": "neutral"},
			{"name": "deploy", "conclusion": "timed_out", "html_url": "https://github.com/acme/product/actions/runs/1/job/12"},
		}})
	})
	failed, err := c.FailedJobs(context.Background(), "acme", "product", 1)
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(failed) != 2 {
		t.Fatalf("expected lint and deploy, got %v", failed)
	}
	// skipped and neutral are the normal shape of a conditional CI.
	for _, f := range failed {
		if strings.HasPrefix(f.Name, "matrix") || strings.HasPrefix(f.Name, "codeql") {
			t.Errorf("skipped/neutral must not count as failures: %v", failed)
		}
	}
	// The diagnosis points at the line, not only at the lane: the job's page
	// and the FIRST failed step within it, in GitHub's own anchor grammar.
	if got := failed[1]; got.Name != "lint" || got.Step != "Run eslint" || got.StepNumber != 4 ||
		got.StepURL != "https://github.com/acme/product/actions/runs/1/job/11#step:4:1" {
		t.Errorf("lint should carry its failing step, got %+v", got)
	}
	if got := failed[0]; got.Name != "deploy" || got.Step != "" || got.StepURL != "" {
		t.Errorf("a job with no step detail carries none, got %+v", got)
	}
	if names := FailedJobNames(failed); strings.Join(names, ", ") != "deploy (timed_out), lint (failure)" {
		t.Errorf("the diagnosis line still reads as it did: %v", names)
	}
}

// A convergence watch can outlive a minted token by an hour, so a client with
// a TokenFn asks for the credential on every request.
func TestClientAsksForTheTokenOnEveryRequest(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "acme/product", "clone_url": "https://github.com/acme/product.git"})
	}))
	defer srv.Close()
	n := 0
	c := &Client{Token: "fallback", APIBase: srv.URL, TokenFn: func() string { n++; return fmt.Sprintf("tok-%d", n) }}
	for i := 0; i < 2; i++ {
		if _, err := c.EnsureRepo(context.Background(), "acme", "product", true, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(auths) != 2 || auths[0] == auths[1] {
		t.Fatalf("each request should carry the token asked for then, got %v", auths)
	}
	empty := &Client{Token: "fallback", APIBase: srv.URL, TokenFn: func() string { return "" }}
	if _, err := empty.EnsureRepo(context.Background(), "acme", "product", true, true); err != nil {
		t.Fatal(err)
	}
	if got := auths[len(auths)-1]; got != "Bearer fallback" {
		t.Fatalf("an empty TokenFn answer falls back to Token, got %q", got)
	}
}

// resetOnce drops the first n connections without a response — what a
// sandbox's network did to a convergence watch — and answers after that.
func resetOnce(t *testing.T, n int, answer func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= n {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		answer(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// THE WATCH THAT DIED ON ONE PACKET. Phase 02's convergence watch polled
// GitHub, one read came back "connection reset by peer", and the build ended
// with its pull request merged and its CI running fine.
func TestAReadThatIsResetIsAskedAgain(t *testing.T) {
	prev := githubretry.Backoff
	githubretry.Backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { githubretry.Backoff = prev }()
	srv, calls := resetOnce(t, 1, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]any{
			{"id": 7, "status": "completed", "conclusion": "success", "head_sha": "abc"},
		}})
	})
	c := &Client{Token: "t", APIBase: srv.URL}
	run, err := c.RunForCommit(context.Background(), "acme", "product", "main", "abc")
	if err != nil {
		t.Fatalf("one reset connection ended the watch: %v", err)
	}
	if run == nil || run.ID != 7 || *calls != 2 {
		t.Fatalf("run = %+v after %d call(s), want run 7 on the second", run, *calls)
	}
}

// A create that was reset may have happened; asking again is not this
// client's call to make.
func TestAWriteThatIsResetIsNotRepeated(t *testing.T) {
	srv, calls := resetOnce(t, 5, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := &Client{Token: "t", APIBase: srv.URL}
	if err := c.RerunFailed(context.Background(), "acme", "product", 7); err == nil {
		t.Fatal("a reset write should surface, not succeed")
	}
	if *calls != 1 {
		t.Fatalf("a write was sent %d times", *calls)
	}
}
