package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
