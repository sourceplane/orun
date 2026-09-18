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

// Phase 01 is re-run like any other phase, so the second run must find the
// repository the first one made rather than fail on a name collision.
func TestEnsureRepoCreatesWhenAbsent(t *testing.T) {
	var postedTo string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
			return
		}
		postedTo = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name": "acme/product", "clone_url": "https://github.com/acme/product.git",
		})
	})
	res, err := c.EnsureRepo(context.Background(), "acme", "product", true, true)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !res.Created {
		t.Error("a fresh repo should be reported as created")
	}
	if postedTo != "/orgs/acme/repos" {
		t.Errorf("an org create must use the org endpoint, got %q", postedTo)
	}
}

// GitHub reads the owner of POST /user/repos off the token's own identity, so
// the two endpoints are not interchangeable.
func TestEnsureRepoUsesTheUserEndpointOutsideAnOrg(t *testing.T) {
	var postedTo string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		postedTo = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "me/product"})
	})
	if _, err := c.EnsureRepo(context.Background(), "me", "product", true, false); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if postedTo != "/user/repos" {
		t.Errorf("a personal create must use /user/repos, got %q", postedTo)
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
			{"name": "lint", "conclusion": "failure"},
			{"name": "matrix", "conclusion": "skipped"},
			{"name": "codeql", "conclusion": "neutral"},
			{"name": "deploy", "conclusion": "timed_out"},
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
		if strings.HasPrefix(f, "matrix") || strings.HasPrefix(f, "codeql") {
			t.Errorf("skipped/neutral must not count as failures: %v", failed)
		}
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
