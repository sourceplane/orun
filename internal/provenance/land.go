package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Landing a PR the pen opened (orun-bootstrap-engine BE-O1b).
//
// Open() is the gesture that starts a landing; this is the one that finishes
// it: wait for the checks to settle, merge, and return the working tree to a
// pulled base. A baseline's phase does exactly this between "apply" and
// "converge", and did it in ~250 lines of shell because the binary offered the
// first half and not the second.
//
// Three behaviours are load-bearing and are the reason this is not three lines
// of API calls:
//
//   - A repository with NO checks yet passes. The first phase of a bootstrap
//     creates the repo and its CI in the same landing, so at merge time there
//     is nothing to wait for. Waiting forever there is the single most common
//     way an unattended bootstrap hangs.
//   - A check that is merely QUEUED is not a passing check. Polling stops when
//     every run has a conclusion, not when none has failed yet.
//   - A refused merge reports WHY. "422" is not an answer an operator can act
//     on; "not mergeable: behind base" and "blocked by required review" are.

// LandRequest is one landing.
type LandRequest struct {
	// Number is the pull request to land.
	Number int
	// Base is the branch to return to afterwards. Default "main".
	Base string
	// CheckTimeout bounds the wait for checks to settle. Zero means do not
	// wait at all — the caller has decided the convergence it watches later is
	// the real gate.
	CheckTimeout time.Duration
	// PollInterval between check polls. Default 15s.
	PollInterval time.Duration
	// MergeMethod is squash | merge | rebase. Default squash.
	MergeMethod string
}

// LandResult is what the landing did.
type LandResult struct {
	Merged bool
	// SHA is the head commit the checks were read from.
	SHA string
	// ChecksSeen is how many check runs were observed. Zero is a real and
	// expected answer on a repository whose CI has not landed yet.
	ChecksSeen int
	// MergeSHA is the resulting commit on the base branch.
	MergeSHA string
}

// checkRun is the subset of a GitHub check run this cares about.
type checkRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// Land waits for the PR's checks, merges it, and returns to a pulled base.
func (p *Pen) Land(ctx context.Context, req LandRequest) (*LandResult, error) {
	if req.Number <= 0 {
		return nil, fmt.Errorf("provenance: land needs the pull request number")
	}
	base := req.Base
	if base == "" {
		base = "main"
	}
	method := req.MergeMethod
	if method == "" {
		method = "squash"
	}
	switch method {
	case "squash", "merge", "rebase":
	default:
		return nil, fmt.Errorf("provenance: merge method %q is not squash, merge or rebase", method)
	}

	owner, repo, err := p.originRepo(ctx)
	if err != nil {
		return nil, err
	}
	token := ""
	if p.Token != nil {
		token = p.Token()
	}
	if token == "" {
		return nil, fmt.Errorf("provenance: landing needs a GitHub credential (GITHUB_TOKEN / GH_TOKEN / gh auth)")
	}

	head, err := p.prHeadSHA(ctx, owner, repo, req.Number, token)
	if err != nil {
		return nil, err
	}
	out := &LandResult{SHA: head}

	if req.CheckTimeout > 0 {
		seen, err := p.waitForChecks(ctx, owner, repo, head, token, req)
		out.ChecksSeen = seen
		if err != nil {
			return out, err
		}
	}

	mergeSHA, err := p.merge(ctx, owner, repo, req.Number, head, method, token)
	if err != nil {
		return out, err
	}
	out.Merged = true
	out.MergeSHA = mergeSHA

	// Back to a pulled base, so the next phase applies onto the landing it just
	// made rather than onto the tree as it was before.
	if _, err := p.git(ctx, "checkout", base); err != nil {
		return out, fmt.Errorf("provenance: merged, but returning to %s failed: %w", base, err)
	}
	if _, err := p.git(ctx, "pull", "--ff-only", "origin", base); err != nil {
		return out, fmt.Errorf("provenance: merged, but pulling %s failed: %w", base, err)
	}
	return out, nil
}

// waitForChecks polls until every check run on the head commit has a
// conclusion, and reports how many were seen.
func (p *Pen) waitForChecks(ctx context.Context, owner, repo, sha, token string, req LandRequest) (int, error) {
	interval := req.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	deadline := time.Now().Add(req.CheckTimeout)
	for {
		runs, err := p.checkRuns(ctx, owner, repo, sha, token)
		if err != nil {
			return 0, err
		}
		// No checks at all is a pass, not a wait. See the file comment.
		if len(runs) == 0 {
			return 0, nil
		}
		var pending, failed []string
		for _, r := range runs {
			if r.Status != "completed" {
				pending = append(pending, r.Name)
				continue
			}
			switch r.Conclusion {
			case "success", "neutral", "skipped":
			default:
				failed = append(failed, fmt.Sprintf("%s (%s)", r.Name, r.Conclusion))
			}
		}
		if len(failed) > 0 {
			sort.Strings(failed)
			return len(runs), fmt.Errorf("provenance: checks failed on %s: %s", shortSHA(sha), strings.Join(failed, ", "))
		}
		if len(pending) == 0 {
			return len(runs), nil
		}
		if time.Now().After(deadline) {
			sort.Strings(pending)
			return len(runs), fmt.Errorf("provenance: checks still running after %s: %s", req.CheckTimeout, strings.Join(pending, ", "))
		}
		select {
		case <-ctx.Done():
			return len(runs), ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (p *Pen) checkRuns(ctx context.Context, owner, repo, sha, token string) ([]checkRun, error) {
	var payload struct {
		CheckRuns []checkRun `json:"check_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?per_page=100", owner, repo, sha)
	if err := p.apiJSON(ctx, http.MethodGet, path, token, nil, &payload); err != nil {
		return nil, err
	}
	return payload.CheckRuns, nil
}

func (p *Pen) prHeadSHA(ctx context.Context, owner, repo string, number int, token string) (string, error) {
	var payload struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	if err := p.apiJSON(ctx, http.MethodGet, path, token, nil, &payload); err != nil {
		return "", err
	}
	if payload.Head.SHA == "" {
		return "", fmt.Errorf("provenance: pull request #%d has no head commit", number)
	}
	return payload.Head.SHA, nil
}

func (p *Pen) merge(ctx context.Context, owner, repo string, number int, sha, method, token string) (string, error) {
	body, _ := json.Marshal(map[string]any{"merge_method": method, "sha": sha})
	var payload struct {
		Merged  bool   `json:"merged"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, number)
	if err := p.apiJSON(ctx, http.MethodPut, path, token, body, &payload); err != nil {
		return "", err
	}
	if !payload.Merged {
		msg := payload.Message
		if msg == "" {
			msg = "GitHub reported the merge did not happen"
		}
		return "", fmt.Errorf("provenance: #%d was not merged: %s", number, msg)
	}
	return payload.SHA, nil
}

// apiJSON performs one GitHub API call and decodes its body. A non-2xx carries
// GitHub's own message through, because "422" alone is not something an
// operator can act on.
func (p *Pen) apiJSON(ctx context.Context, method, path, token string, body []byte, out any) error {
	apiBase := p.APIBase
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("provenance: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &ghErr)
		detail := ghErr.Message
		if detail == "" {
			detail = truncate(string(raw))
		}
		return fmt.Errorf("provenance: GitHub answered %d for %s: %s", resp.StatusCode, path, detail)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("provenance: decoding %s: %w", path, err)
	}
	return nil
}

// originRepo resolves owner/repo from the origin remote.
func (p *Pen) originRepo(ctx context.Context) (string, string, error) {
	remote, err := p.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	m := githubRemoteRe.FindStringSubmatch(remote)
	if m == nil {
		return "", "", fmt.Errorf("provenance: origin %q is not a github.com repository", remote)
	}
	return m[1], m[2], nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
