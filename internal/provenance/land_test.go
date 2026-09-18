package provenance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// landHarness stands up a fake GitHub and a fake git, so a landing is testable
// end to end with no network and no repository.
type landHarness struct {
	mu        sync.Mutex
	checkSets [][]checkRun // one entry per poll; the last repeats
	polls     int
	// workflowSets are the Actions runs for the head, one entry per poll; the
	// last repeats. Empty means the repository has no workflows.
	workflowSets [][]workflowRun
	wpolls       int
	merged       bool
	mergeBody    map[string]any
	gitCalls     []string
	mergeCode    int
	mergeMsg     string
	// A token cut to a repository's write tier reads Actions but not Checks.
	checksForbidden    bool
	workflowsForbidden bool
	// auths is the credential each request carried, with its path.
	auths []string
}

func (h *landHarness) pen(t *testing.T) *Pen {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.auths = append(h.auths, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		forbid := func() {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Resource not accessible by integration"})
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/runs") && h.workflowsForbidden:
			forbid()
		case strings.HasSuffix(r.URL.Path, "/check-runs") && h.checksForbidden:
			forbid()
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			var runs []workflowRun
			if len(h.workflowSets) > 0 {
				idx := h.wpolls
				if idx >= len(h.workflowSets) {
					idx = len(h.workflowSets) - 1
				}
				runs = h.workflowSets[idx]
			}
			h.wpolls++
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			idx := h.polls
			if idx >= len(h.checkSets) {
				idx = len(h.checkSets) - 1
			}
			h.polls++
			var runs []checkRun
			if idx >= 0 && len(h.checkSets) > 0 {
				runs = h.checkSets[idx]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": runs})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			h.mergeBody = body
			if h.mergeCode != 0 {
				w.WriteHeader(h.mergeCode)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": h.mergeMsg})
				return
			}
			h.merged = true
			_ = json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": "mergedsha"})
		default: // the PR read
			_ = json.NewEncoder(w).Encode(map[string]any{"head": map[string]any{"sha": "headsha0123456789"}})
		}
	}))
	t.Cleanup(srv.Close)
	return &Pen{
		Workdir: ".",
		APIBase: srv.URL,
		Token:   func() string { return "t0ken" },
		RunGit: func(_ context.Context, args ...string) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			joined := strings.Join(args, " ")
			h.gitCalls = append(h.gitCalls, joined)
			if args[0] == "remote" {
				return "https://github.com/acme/product.git", nil
			}
			return "", nil
		},
	}
}

func req(n int) LandRequest {
	return LandRequest{Number: n, CheckTimeout: time.Second, PollInterval: time.Millisecond, CIGrace: 20 * time.Millisecond}
}

// A freshly scaffolded repository has no CI yet. Waiting forever there is the
// single most common way an unattended bootstrap hangs.
func TestLandPassesWhenTheRepoHasNoChecksYet(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{}}}
	res, err := h.pen(t).Land(context.Background(), req(7))
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !res.Merged {
		t.Error("a repo with no checks should merge")
	}
	if res.ChecksSeen != 0 {
		t.Errorf("ChecksSeen should be 0, got %d", res.ChecksSeen)
	}
}

func TestLandWaitsForAQueuedCheckToConclude(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{
		{{Name: "test", Status: "queued"}},
		{{Name: "test", Status: "in_progress"}},
		{{Name: "test", Status: "completed", Conclusion: "success"}},
	}}
	res, err := h.pen(t).Land(context.Background(), req(7))
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !res.Merged {
		t.Error("should merge once the check concludes")
	}
	if h.polls < 3 {
		t.Errorf("should have polled until conclusive, polled %d", h.polls)
	}
}

// A check that has not failed YET is not a check that passed.
func TestLandDoesNotMergeWhileAChckIsStillRunning(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{{Name: "slow", Status: "in_progress"}}}}
	p := h.pen(t)
	r := req(7)
	r.CheckTimeout = 5 * time.Millisecond
	_, err := p.Land(context.Background(), r)
	if err == nil {
		t.Fatal("expected a timeout error rather than a merge")
	}
	if !strings.Contains(err.Error(), "slow") {
		t.Errorf("the error should name what it waited on; got %v", err)
	}
	if h.merged {
		t.Error("must not merge on timeout")
	}
}

func TestLandRefusesOnAFailedCheckAndNamesIt(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{
		{Name: "test", Status: "completed", Conclusion: "success"},
		{Name: "lint", Status: "completed", Conclusion: "failure"},
	}}}
	_, err := h.pen(t).Land(context.Background(), req(7))
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Errorf("the error should name the failing check; got %v", err)
	}
	if h.merged {
		t.Error("must not merge when a check failed")
	}
}

// neutral and skipped are not failures — a skipped matrix leg is the normal
// shape of a conditional CI, and treating it as red would block every landing.
func TestLandTreatsNeutralAndSkippedAsPassing(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{
		{Name: "codeql", Status: "completed", Conclusion: "neutral"},
		{Name: "matrix", Status: "completed", Conclusion: "skipped"},
	}}}
	if _, err := h.pen(t).Land(context.Background(), req(7)); err != nil {
		t.Fatalf("neutral/skipped must not block a landing: %v", err)
	}
}

func TestLandSurfacesGitHubsRefusalMessage(t *testing.T) {
	h := &landHarness{
		checkSets: [][]checkRun{{}},
		mergeCode: 405,
		mergeMsg:  "Pull Request is not mergeable",
	}
	_, err := h.pen(t).Land(context.Background(), req(7))
	if err == nil {
		t.Fatal("expected the refusal to surface")
	}
	// "405" alone is not something an operator can act on.
	if !strings.Contains(err.Error(), "not mergeable") {
		t.Errorf("GitHub's own message must come through; got %v", err)
	}
}

func TestLandReturnsToAPulledBase(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{}}}
	if _, err := h.pen(t).Land(context.Background(), LandRequest{Number: 7, Base: "trunk"}); err != nil {
		t.Fatalf("land: %v", err)
	}
	joined := strings.Join(h.gitCalls, " | ")
	if !strings.Contains(joined, "checkout trunk") {
		t.Errorf("should return to the base branch; git calls: %s", joined)
	}
	if !strings.Contains(joined, "pull --ff-only origin trunk") {
		t.Errorf("should pull the base fast-forward only; git calls: %s", joined)
	}
}

func TestLandPinsTheMergeToTheHeadItChecked(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{{Name: "t", Status: "completed", Conclusion: "success"}}}}
	if _, err := h.pen(t).Land(context.Background(), req(7)); err != nil {
		t.Fatalf("land: %v", err)
	}
	// Merging without pinning the SHA would merge whatever arrived while the
	// checks were running — including a push that no check ever saw.
	if got := fmt.Sprint(h.mergeBody["sha"]); got != "headsha0123456789" {
		t.Errorf("merge should pin the checked head; sent sha=%q", got)
	}
	if got := fmt.Sprint(h.mergeBody["merge_method"]); got != "squash" {
		t.Errorf("default merge method should be squash; sent %q", got)
	}
}

func TestLandRejectsAnUnknownMergeMethod(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{}}}
	r := req(7)
	r.MergeMethod = "fast-forward"
	if _, err := h.pen(t).Land(context.Background(), r); err == nil {
		t.Fatal("expected an error for an unsupported merge method")
	}
}

func TestLandNeedsACredential(t *testing.T) {
	p := &Pen{Workdir: ".", Token: func() string { return "" },
		RunGit: func(context.Context, ...string) (string, error) { return "https://github.com/a/b.git", nil }}
	if _, err := p.Land(context.Background(), req(7)); err == nil {
		t.Fatal("landing anonymously must be refused, not silently skipped")
	}
}

// GitHub registers a pull request's CI seconds after the PR exists, so "no
// checks" read the moment it opens would merge every PR a bootstrap lands
// without waiting for a single lane. (Found reading this while moving the
// bootstrap's landings from wait: false to wait: true.) CI that turns up
// inside the grace is waited on.
func TestLandWaitsForCIThatRegistersAfterThePROpens(t *testing.T) {
	h := &landHarness{
		checkSets: [][]checkRun{{}, {}, {}, {{Name: "plan", Status: "completed", Conclusion: "success"}}},
		workflowSets: [][]workflowRun{{}, {}, {},
			{{Name: "CI", Status: "in_progress"}},
			{{Name: "CI", Status: "completed", Conclusion: "success"}}},
	}
	r := req(7)
	r.CIGrace = time.Second
	res, err := h.pen(t).Land(context.Background(), r)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !res.Merged || res.ChecksSeen == 0 {
		t.Errorf("should merge after the late CI concluded; merged=%v seen=%d", res.Merged, res.ChecksSeen)
	}
	if h.wpolls < 5 {
		t.Errorf("merged before the CI that registered late had concluded (polled %d)", h.wpolls)
	}
}

// A plan-then-matrix workflow creates its lanes only after the plan job ends,
// so for a moment the only check run is a green `plan`. The workflow run is
// what says whether the lanes are done.
func TestLandWaitsForTheWorkflowRunNotJustTheChecksThatExistSoFar(t *testing.T) {
	h := &landHarness{
		checkSets: [][]checkRun{{{Name: "plan", Status: "completed", Conclusion: "success"}}},
		workflowSets: [][]workflowRun{
			{{Name: "CI", Status: "in_progress"}},
			{{Name: "CI", Status: "in_progress"}},
			{{Name: "CI", Status: "completed", Conclusion: "success"}},
		},
	}
	res, err := h.pen(t).Land(context.Background(), req(7))
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !res.Merged {
		t.Fatal("should merge once the workflow run completes")
	}
	if h.wpolls < 3 {
		t.Errorf("merged on a green plan while the workflow was still running (polled %d)", h.wpolls)
	}
}

func TestLandRefusesWhenTheWorkflowRunFails(t *testing.T) {
	h := &landHarness{
		checkSets:    [][]checkRun{{{Name: "plan", Status: "completed", Conclusion: "success"}}},
		workflowSets: [][]workflowRun{{{Name: "CI", Status: "completed", Conclusion: "failure"}}},
	}
	_, err := h.pen(t).Land(context.Background(), req(7))
	if err == nil || !strings.Contains(err.Error(), "workflow CI (failure)") {
		t.Fatalf("want a refusal naming the failed workflow, got %v", err)
	}
	if h.merged {
		t.Error("must not merge over a failed workflow run")
	}
}

// With no CI at all, the landing still merges — once the grace has passed.
func TestLandPassesARepoWithNoCIOnceTheGraceHasPassed(t *testing.T) {
	h := &landHarness{checkSets: [][]checkRun{{}}}
	r := req(7)
	r.CIGrace = 30 * time.Millisecond
	started := time.Now()
	res, err := h.pen(t).Land(context.Background(), r)
	if err != nil || !res.Merged {
		t.Fatalf("a repo with no CI should merge; merged=%v err=%v", res != nil && res.Merged, err)
	}
	if time.Since(started) < r.CIGrace {
		t.Error("concluded \"no CI\" before the grace had passed")
	}
}

// A GitHub App token cut to a repository's write tier — what a platform
// sandbox mints — reads Actions but not Checks. The check-runs listing refuses
// it, and a landing that treated that as fatal could never land a product whose
// CI is GitHub Actions, which is fully described by its workflow runs.
func TestLandWithoutChecksReadWaitsOnTheWorkflowRuns(t *testing.T) {
	h := &landHarness{
		checksForbidden: true,
		workflowSets: [][]workflowRun{
			{{Name: "CI", Status: "in_progress"}},
			{{Name: "CI", Status: "in_progress"}},
			{{Name: "CI", Status: "completed", Conclusion: "success"}},
		},
	}
	res, err := h.pen(t).Land(context.Background(), req(7))
	if err != nil {
		t.Fatalf("a token that reads Actions can land on its workflow runs: %v", err)
	}
	if !res.Merged {
		t.Fatal("should merge once the workflow run completes")
	}
	if h.wpolls < 3 {
		t.Errorf("merged before the workflow run finished (polled %d)", h.wpolls)
	}
}

func TestLandWithoutChecksReadStillRefusesAFailedWorkflow(t *testing.T) {
	h := &landHarness{
		checksForbidden: true,
		workflowSets:    [][]workflowRun{{{Name: "CI", Status: "completed", Conclusion: "failure"}}},
	}
	_, err := h.pen(t).Land(context.Background(), req(7))
	if err == nil || !strings.Contains(err.Error(), "workflow CI (failure)") {
		t.Fatalf("want a refusal naming the failed workflow, got %v", err)
	}
	if h.merged {
		t.Error("must not merge over a failed workflow run")
	}
}

// Neither readable: there is nothing to wait on honestly, and merging blind is
// the one thing a verified landing must never do.
func TestLandThatCanReadNeitherRefuses(t *testing.T) {
	h := &landHarness{checksForbidden: true, workflowsForbidden: true}
	_, err := h.pen(t).Land(context.Background(), req(7))
	if err == nil {
		t.Fatal("a landing that can read neither checks nor workflow runs must not merge")
	}
	if h.merged {
		t.Error("merged blind")
	}
}

// A minted credential lasts an hour and the check wait can last as long, so
// every request asks for the token again rather than reusing the first.
func TestLandAsksForTheTokenOnEveryRequest(t *testing.T) {
	h := &landHarness{workflowSets: [][]workflowRun{
		{{Name: "CI", Status: "in_progress"}},
		{{Name: "CI", Status: "completed", Conclusion: "success"}},
	}}
	pen := h.pen(t)
	var mu sync.Mutex
	issued := 0
	pen.Token = func() string {
		mu.Lock()
		defer mu.Unlock()
		issued++
		return fmt.Sprintf("tok-%d", issued)
	}
	if _, err := pen.Land(context.Background(), req(7)); err != nil {
		t.Fatalf("land: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	var mergeAuth string
	for _, a := range h.auths {
		parts := strings.SplitN(a, " ", 3)
		seen[parts[2]] = true
		if strings.HasSuffix(parts[1], "/merge") {
			mergeAuth = parts[2]
		}
	}
	if len(seen) < 3 {
		t.Fatalf("every request should ask for the token; saw %d distinct across %d requests", len(seen), len(h.auths))
	}
	if mergeAuth == "Bearer tok-1" || mergeAuth == "" {
		t.Fatalf("the merge, the last request, went out with %q — the token the landing started with", mergeAuth)
	}
}
