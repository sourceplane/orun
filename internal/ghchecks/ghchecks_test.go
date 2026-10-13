package ghchecks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type call struct {
	Method, Path string
	Body         map[string]any
}

func server(t *testing.T, status int) (*httptest.Server, *[]call) {
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, call{r.Method, r.URL.Path, body})
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestStartThenFinishPatchesTheSameRun(t *testing.T) {
	srv, calls := server(t, 201)
	r := &Reporter{API: srv.URL, Repo: "o/r", Token: "tok", SHA: "abc", DetailsURL: "https://x/runs/1"}
	r.Start("a.stage", "a · stage · Deploy")
	r.Finish("a.stage", "a · stage · Deploy", Success, "")
	if len(*calls) != 2 {
		t.Fatalf("calls = %+v", *calls)
	}
	c0, c1 := (*calls)[0], (*calls)[1]
	if c0.Method != "POST" || c0.Path != "/repos/o/r/check-runs" || c0.Body["name"] != "a · stage · Deploy" || c0.Body["head_sha"] != "abc" || c0.Body["status"] != "in_progress" {
		t.Fatalf("start = %+v", c0)
	}
	if c1.Method != "PATCH" || c1.Path != "/repos/o/r/check-runs/42" || c1.Body["conclusion"] != "success" {
		t.Fatalf("finish = %+v", c1)
	}
}

func TestFinishWithoutStartCreatesCompleted(t *testing.T) {
	srv, calls := server(t, 201)
	r := &Reporter{API: srv.URL, Repo: "o/r", Token: "tok", SHA: "abc"}
	r.Finish("b.stage", "b · stage · Deploy", Cancelled, "upstream failed")
	c := (*calls)[0]
	if c.Method != "POST" || c.Body["status"] != "completed" || c.Body["conclusion"] != "cancelled" || c.Body["name"] != "b · stage · Deploy" {
		t.Fatalf("call = %+v", c)
	}
}

func TestPermissionErrorWarnsOnceAndStops(t *testing.T) {
	srv, calls := server(t, 403)
	var warn bytes.Buffer
	r := &Reporter{API: srv.URL, Repo: "o/r", Token: "tok", SHA: "abc", Warn: &warn}
	r.Start("a", "A")
	r.Finish("a", "A", Success, "")
	r.Start("b", "B")
	if len(*calls) != 1 {
		t.Fatalf("posting must stop after a 403: %d calls", len(*calls))
	}
	if strings.Count(warn.String(), "check runs") != 1 || !strings.Contains(warn.String(), "checks: write") {
		t.Fatalf("warn = %q", warn.String())
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_SHA", "merge")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "7")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	r := FromEnv(nil)
	if r == nil || r.SHA != "merge" || r.DetailsURL != "https://github.com/o/r/actions/runs/7/attempts/2" {
		t.Fatalf("reporter = %+v", r)
	}
	t.Setenv("ORUN_CHECK_RUNS", "0")
	if FromEnv(nil) != nil {
		t.Fatal("ORUN_CHECK_RUNS=0 must turn posting off")
	}
	t.Setenv("ORUN_CHECK_RUNS", "")
	t.Setenv("GITHUB_TOKEN", "")
	if FromEnv(nil) != nil {
		t.Fatal("no token, no reporter")
	}
}
