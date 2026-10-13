// Package ghchecks posts one GitHub check run per job of a multi-job lane
// (specs/orun-lanes §9.2), so branch protection keeps depending on the job
// titles it knows when several jobs share one GitHub job.
//
// Posting is best effort: a failure is a warning, never a job failure.
package ghchecks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Conclusions a finished job can carry.
const (
	Success   = "success"
	Failure   = "failure"
	Cancelled = "cancelled"
)

// Reporter creates and completes check runs on one commit.
type Reporter struct {
	API        string
	Repo       string
	Token      string
	SHA        string
	DetailsURL string
	Client     *http.Client
	Warn       io.Writer

	mu     sync.Mutex
	ids    map[string]int64
	warned bool
	off    bool // set after a permission error: every later call would fail too
}

// FromEnv builds a Reporter from the GitHub Actions environment. It returns
// nil outside Actions, without a GITHUB_TOKEN, or when ORUN_CHECK_RUNS=0.
func FromEnv(warn io.Writer) *Reporter {
	if !strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true") || os.Getenv("ORUN_CHECK_RUNS") == "0" {
		return nil
	}
	token := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_REPOSITORY")
	sha := headSHA()
	if token == "" || repo == "" || sha == "" {
		return nil
	}
	api := strings.TrimRight(os.Getenv("GITHUB_API_URL"), "/")
	if api == "" {
		api = "https://api.github.com"
	}
	details := ""
	if server, run := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_RUN_ID"); server != "" && run != "" {
		details = fmt.Sprintf("%s/%s/actions/runs/%s", strings.TrimRight(server, "/"), repo, run)
		if attempt := os.Getenv("GITHUB_RUN_ATTEMPT"); attempt != "" {
			details += "/attempts/" + attempt
		}
	}
	return &Reporter{API: api, Repo: repo, Token: token, SHA: sha, DetailsURL: details, Warn: warn}
}

// headSHA is the commit under test: the pull request head for pull_request
// events (GITHUB_SHA is then the merge commit), else GITHUB_SHA.
func headSHA() string {
	if strings.HasPrefix(os.Getenv("GITHUB_EVENT_NAME"), "pull_request") {
		if path := os.Getenv("GITHUB_EVENT_PATH"); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				var ev struct {
					PullRequest struct {
						Head struct {
							SHA string `json:"sha"`
						} `json:"head"`
					} `json:"pull_request"`
				}
				if json.Unmarshal(data, &ev) == nil && ev.PullRequest.Head.SHA != "" {
					return ev.PullRequest.Head.SHA
				}
			}
		}
	}
	return os.Getenv("GITHUB_SHA")
}

// Start opens an in-progress check run named name for jobID.
func (r *Reporter) Start(jobID, name string) {
	if r == nil || r.disabled() {
		return
	}
	body := map[string]any{
		"name":        name,
		"head_sha":    r.SHA,
		"status":      "in_progress",
		"started_at":  time.Now().UTC().Format(time.RFC3339),
		"external_id": jobID,
	}
	if r.DetailsURL != "" {
		body["details_url"] = r.DetailsURL
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := r.call(http.MethodPost, "/repos/"+r.Repo+"/check-runs", body, &out); err != nil {
		r.warn(name, err)
		return
	}
	r.mu.Lock()
	if r.ids == nil {
		r.ids = map[string]int64{}
	}
	r.ids[jobID] = out.ID
	r.mu.Unlock()
}

// Finish completes jobID's check run with conclusion, creating it already
// completed when Start never ran (a resumed or blocked job).
func (r *Reporter) Finish(jobID, name, conclusion, summary string) {
	if r == nil || r.disabled() {
		return
	}
	r.mu.Lock()
	id, ok := r.ids[jobID]
	r.mu.Unlock()
	body := map[string]any{
		"status":       "completed",
		"conclusion":   conclusion,
		"completed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if summary != "" {
		body["output"] = map[string]string{"title": name, "summary": summary}
	}
	var err error
	if ok {
		err = r.call(http.MethodPatch, fmt.Sprintf("/repos/%s/check-runs/%d", r.Repo, id), body, nil)
	} else {
		body["name"] = name
		body["head_sha"] = r.SHA
		body["external_id"] = jobID
		if r.DetailsURL != "" {
			body["details_url"] = r.DetailsURL
		}
		err = r.call(http.MethodPost, "/repos/"+r.Repo+"/check-runs", body, nil)
	}
	if err != nil {
		r.warn(name, err)
	}
}

func (r *Reporter) call(method, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.API+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (r *Reporter) disabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.off
}

func (r *Reporter) warn(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, code := range []string{": 401", ": 403", ": 404"} {
		if strings.Contains(err.Error(), code) {
			r.off = true
		}
	}
	if r.Warn == nil || r.warned {
		return
	}
	r.warned = true
	hint := ""
	if strings.Contains(err.Error(), ": 403") {
		hint = " (the workflow needs `permissions: checks: write`; ORUN_CHECK_RUNS=0 turns posting off)"
	}
	fmt.Fprintf(r.Warn, "⚠ check runs: could not post %q: %v%s — continuing without per-job checks\n", name, err, hint)
}
