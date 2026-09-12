package flow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fakeGitHub serves the two API shapes FetchRemote uses: raw contents and
// commit resolution.
func fakeGitHub(t *testing.T, flowBody string, wantAuth string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Authorization") != "Bearer "+wantAuth {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/contents/"):
			if r.URL.Query().Get("ref") == "missing" {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			w.Write([]byte(flowBody))
		case strings.Contains(r.URL.Path, "/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "abc123def4567890abc123def4567890abc123de"})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

const remoteFlowBody = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: remote-test
steps:
  - name: hello
    run: ["/bin/sh", "-c", "echo hi"]
`

func TestFetchRemote_GitHubRefWithAuth(t *testing.T) {
	srv := fakeGitHub(t, remoteFlowBody, "tok123")
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "tok123")

	path, meta, cleanup, err := FetchRemote(context.Background(), "github:acme/base@v1.2.3//flows/phases/02-foundation/workflow.yaml")
	if err != nil {
		t.Fatalf("FetchRemote: %v", err)
	}
	defer cleanup()
	if meta.Repo != "acme/base" || meta.Ref != "v1.2.3" {
		t.Errorf("meta = %+v", meta)
	}
	if len(meta.SHA) != 40 {
		t.Errorf("sha not resolved: %q", meta.SHA)
	}
	data, _ := os.ReadFile(path)
	if string(data) != remoteFlowBody {
		t.Errorf("body mismatch: %q", data)
	}
	if wf, err := Parse(data); err != nil || wf.Metadata.Name != "remote-test" {
		t.Errorf("fetched flow does not parse: %v", err)
	}
}

func TestFetchRemote_MissingRefFailsLoudly(t *testing.T) {
	srv := fakeGitHub(t, remoteFlowBody, "")
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	_, _, _, err := FetchRemote(context.Background(), "github:acme/base@missing//flow.yaml")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error should hint at GITHUB_TOKEN for unauthenticated fetches: %v", err)
	}
}

func TestFetchRemote_InvalidRefShapes(t *testing.T) {
	for _, ref := range []string{"github:acme//flow.yaml", "github:acme/base@v1", "github:onlyrepo//x.yaml"} {
		if _, _, _, err := FetchRemote(context.Background(), ref); err == nil {
			t.Errorf("expected error for %q", ref)
		}
	}
}

func TestRun_ExportsSourceMetaToSteps(t *testing.T) {
	wf := &Workflow{
		Metadata: Metadata{Name: "src-meta"},
		Steps: []Step{
			{Name: "env", Run: []string{"/bin/sh", "-c", "echo repo=$ORUN_FLOW_SOURCE_REPO sha=$ORUN_FLOW_SOURCE_SHA"}},
		},
	}
	res, err := Run(context.Background(), wf, RunOptions{
		Dir:    t.TempDir(),
		Source: &SourceMeta{Repo: "acme/base", Ref: "v1", SHA: "deadbeef"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	st := res.Steps["env"]
	if st == nil || !strings.Contains(st.Stdout, "repo=acme/base") || !strings.Contains(st.Stdout, "sha=deadbeef") {
		t.Fatalf("source meta not in step env: %+v", st)
	}
}

// fakePublicGitHub serves a PUBLIC repo the way GitHub does: anonymous reads
// succeed, but a credential the server does not recognise is rejected with 401
// rather than ignored. That asymmetry is the whole bug — presenting a dead
// token to a repo that needed no token at all turns a working fetch into a
// hard failure. It records how many requests arrived with an Authorization
// header so a test can prove the retry really dropped it.
func fakePublicGitHub(t *testing.T, flowBody string, authed *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			*authed++
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/contents/"):
			w.Write([]byte(flowBody))
		case strings.Contains(r.URL.Path, "/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "abc123def4567890abc123def4567890abc123de"})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

// An expired GITHUB_TOKEN must not break a fetch from a public repo. `orun
// agent serve` seeds that variable once at boot from a ≤1h installation token
// while a baseline build runs for hours, so every re-run of
// `orun workflow run github:sourceplane/cirrus@…` arrived with a dead
// credential and failed 401 against a repository that never needed one.
func TestFetchRemote_ExpiredTokenFallsBackToAnonymous(t *testing.T) {
	authed := 0
	srv := fakePublicGitHub(t, remoteFlowBody, &authed)
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "expired-installation-token")
	t.Setenv("GH_TOKEN", "")

	path, meta, cleanup, err := FetchRemote(context.Background(), "github:sourceplane/cirrus@baseline-v3//flows/agent/workflow.yaml")
	if err != nil {
		t.Fatalf("a dead token must not fail a public fetch: %v", err)
	}
	defer cleanup()
	data, _ := os.ReadFile(path)
	if string(data) != remoteFlowBody {
		t.Errorf("body mismatch: %q", data)
	}
	// Exactly one rejected attempt: the file fetch. The SHA resolve must not
	// re-attach the credential the file fetch just abandoned — if it did, the
	// flow would lose its self-pin to the same dead token.
	if authed != 1 {
		t.Errorf("expected 1 rejected authenticated request, got %d", authed)
	}
	if len(meta.SHA) != 40 {
		t.Errorf("sha not resolved after the anonymous retry: %q", meta.SHA)
	}
}

// The fallback must not paper over a repo that genuinely needs a credential:
// when the anonymous retry fails too, the caller sees the refusal and a hint
// naming the rejected token rather than the misleading "set GITHUB_TOKEN".
func TestFetchRemote_RejectedTokenOnPrivateRepoStillFails(t *testing.T) {
	srv := fakeGitHub(t, remoteFlowBody, "the-right-token")
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "the-wrong-token")
	t.Setenv("GH_TOKEN", "")

	_, _, _, err := FetchRemote(context.Background(), "github:acme/base@v1//flow.yaml")
	if err == nil {
		t.Fatal("expected failure when neither the token nor anonymous access works")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("original refusal should survive: %v", err)
	}
	if !strings.Contains(err.Error(), "was rejected") {
		t.Errorf("hint should name the rejected credential, got: %v", err)
	}
	if strings.Contains(err.Error(), "private repo? set GITHUB_TOKEN") {
		t.Errorf("must not advise setting a token that IS set and refused: %v", err)
	}
}

// A 404 is not a credential problem, so it must not spend a second request.
func TestFetchRemote_NotFoundDoesNotRetryAnonymously(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("GH_TOKEN", "")

	if _, _, _, err := FetchRemote(context.Background(), "github:acme/base@v1//flow.yaml"); err == nil {
		t.Fatal("expected a 404 failure")
	}
	if requests != 1 {
		t.Errorf("a 404 must not trigger the credential retry: %d requests", requests)
	}
}
