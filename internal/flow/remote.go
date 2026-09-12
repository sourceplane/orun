package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// httpStatusError is a non-200 response, carrying the status so a caller can
// react to WHICH refusal it was without pattern-matching an error string. Its
// message is unchanged from the plain fmt.Errorf it replaced.
type httpStatusError struct {
	URL    string
	Status int
	Body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d: %s", e.URL, e.Status, e.Body)
}

// credentialRejected reports whether GitHub refused the credential we sent, as
// distinct from refusing US (404 on a private repo, 403 for rate limits or
// permissions). 401 is the unambiguous case; GitHub also answers 403 with
// "Bad credentials" for some malformed tokens.
func credentialRejected(err error) bool {
	var se *httpStatusError
	if !errors.As(err, &se) {
		return false
	}
	if se.Status == http.StatusUnauthorized {
		return true
	}
	return se.Status == http.StatusForbidden &&
		strings.Contains(strings.ToLower(se.Body), "bad credentials")
}

// SourceMeta records where a remotely fetched workflow came from — repo,
// the ref as given, and the resolved commit SHA. It is exported to every
// run: step as ORUN_FLOW_SOURCE_{REPO,REF,SHA} so a flow can self-pin: fetch
// its own baseline at EXACTLY the commit the flow file was fetched from,
// making one remote reference transitively pin flow + scripts + content.
type SourceMeta struct {
	Repo string // owner/name ("" for plain https URLs)
	Ref  string // ref as requested ("" = default branch)
	SHA  string // resolved commit SHA ("" when resolution was unavailable)
	URL  string // https source URL (plain-URL fetches)
}

// IsRemoteRef reports whether the argument to `workflow run` names a remote
// workflow rather than a local file.
func IsRemoteRef(ref string) bool {
	return strings.HasPrefix(ref, "github:") ||
		strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "http://")
}

// githubAPIBase is overridable for tests (and GHES) via ORUN_GITHUB_API_URL.
func githubAPIBase() string {
	if v := strings.TrimSpace(os.Getenv("ORUN_GITHUB_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}

func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// FetchRemote downloads a remote workflow reference to a temp file and
// returns its local path plus source metadata. Supported schemes:
//
//	github:owner/repo@ref//path/to/workflow.yaml   (ref optional: @ref may be omitted)
//	https://…/workflow.yaml                        (fetched verbatim)
//
// GitHub fetches authenticate with GITHUB_TOKEN/GH_TOKEN when set (required
// for private repos) and also resolve the ref to a commit SHA.
func FetchRemote(ctx context.Context, ref string) (string, *SourceMeta, func(), error) {
	client := &http.Client{Timeout: 60 * time.Second}
	noop := func() {}

	get := func(url string, headers map[string]string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, &httpStatusError{URL: url, Status: resp.StatusCode, Body: firstLine(body)}
		}
		return body, nil
	}

	writeTemp := func(data []byte) (string, func(), error) {
		dir, err := os.MkdirTemp("", "orun-remote-flow-")
		if err != nil {
			return "", noop, err
		}
		p := filepath.Join(dir, "workflow.yaml")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return "", noop, err
		}
		return p, func() { _ = os.RemoveAll(dir) }, nil
	}

	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		body, err := get(ref, nil)
		if err != nil {
			return "", nil, noop, fmt.Errorf("fetch workflow: %w", err)
		}
		p, cleanup, err := writeTemp(body)
		if err != nil {
			return "", nil, noop, err
		}
		return p, &SourceMeta{URL: ref}, cleanup, nil
	}

	// github:owner/repo[@ref]//path
	rest := strings.TrimPrefix(ref, "github:")
	repoAndRef, path, ok := strings.Cut(rest, "//")
	if !ok || path == "" {
		return "", nil, noop, fmt.Errorf("invalid github ref %q: expected github:owner/repo[@ref]//path/to/workflow.yaml", ref)
	}
	repo, gitRef, _ := strings.Cut(repoAndRef, "@")
	if parts := strings.Split(repo, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", nil, noop, fmt.Errorf("invalid github ref %q: repo must be owner/name", ref)
	}

	headers := map[string]string{"Accept": "application/vnd.github.raw"}
	ambient := githubToken()
	if ambient != "" {
		headers["Authorization"] = "Bearer " + ambient
	}
	fileURL := fmt.Sprintf("%s/repos/%s/contents/%s", githubAPIBase(), repo, strings.TrimPrefix(path, "/"))
	if gitRef != "" {
		fileURL += "?ref=" + gitRef
	}
	body, err := get(fileURL, headers)
	// A credential the server REJECTS is worse than no credential at all: for a
	// public repo, sending nothing succeeds where an expired token returns 401.
	// An agent sandbox hits this every time, because `orun agent serve` seeds
	// GITHUB_TOKEN once at boot from a ≤1h installation token while a baseline
	// build runs for hours — so the first `orun workflow run
	// github:sourceplane/cirrus@…` works and every later one fails 401 against
	// a PUBLIC repository that never needed the token. Observed live: a
	// baseline build that had already succeeded once could not be re-run.
	//
	// So: drop the rejected credential and ask again as a stranger. If the repo
	// is public we are simply past it; if it is private the anonymous attempt
	// fails too and the ORIGINAL error is what the caller sees, since "your
	// token was refused" is the useful half of that story.
	if err != nil && ambient != "" && credentialRejected(err) {
		anon := map[string]string{"Accept": "application/vnd.github.raw"}
		if anonBody, anonErr := get(fileURL, anon); anonErr == nil {
			fmt.Fprintf(os.Stderr,
				"orun: the ambient GITHUB_TOKEN was rejected by GitHub; %s is readable without it, continuing unauthenticated\n"+
					"orun: (in an agent sandbox this usually means the seeded token has expired — nothing you need to fix to proceed)\n",
				repo)
			body, err, ambient = anonBody, nil, ""
		}
	}
	if err != nil {
		hint := ""
		switch {
		case ambient == "":
			hint = " (private repo? set GITHUB_TOKEN)"
		case credentialRejected(err):
			hint = " (GITHUB_TOKEN/GH_TOKEN was rejected — it is expired or not valid for this repo; unset it to try anonymously)"
		}
		return "", nil, noop, fmt.Errorf("fetch workflow%s: %w", hint, err)
	}

	meta := &SourceMeta{Repo: repo, Ref: gitRef}
	// Resolve the ref to a commit SHA — best-effort: the flow still runs if
	// resolution fails, it just cannot self-pin to an exact commit.
	shaRef := gitRef
	if shaRef == "" {
		shaRef = "HEAD"
	}
	shaHeaders := map[string]string{"Accept": "application/vnd.github+json"}
	// `ambient`, not githubToken(): if the file fetch above abandoned a rejected
	// credential, re-attaching it here would 401 the SHA resolve too and the
	// flow would silently lose its self-pin for the same dead token.
	if ambient != "" {
		shaHeaders["Authorization"] = "Bearer " + ambient
	}
	if shaBody, shaErr := get(fmt.Sprintf("%s/repos/%s/commits/%s", githubAPIBase(), repo, shaRef), shaHeaders); shaErr == nil {
		var commit struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(shaBody, &commit) == nil {
			meta.SHA = commit.SHA
		}
	}

	p, cleanup, err := writeTemp(body)
	if err != nil {
		return "", nil, noop, err
	}
	return p, meta, cleanup, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
