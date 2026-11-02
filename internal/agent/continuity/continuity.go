// Package continuity keeps a cloud session's state where its successor can
// find it, and gives a successor its predecessor's back
// (saas-session-continuity CT3).
//
// A box ends — its lifetime runs out, it crashes — and everything not pushed
// goes with it: Claude Code's conversation, its reasoning, any uncommitted
// change. The control plane keeps one SessionState object per session
// (orun-cloud agents-worker, CT2). While a session works, the Keeper streams
// the harness's transcript to it and snapshots unpushed work; when a
// successor boots for the same task, Resume writes the predecessor's
// transcript back where Claude Code looks for it (so the driver launches with
// --resume) and re-applies its snapshot.
//
// Everything here is best-effort by contract: a failure is logged and the
// session goes on — it never fails a session that would otherwise run.
//
// The routes (relative to the session's relay base,
// {api}/v1/organizations/{org}/agents/sessions/{id}):
//
//	POST /state/transcript            {harnessSession, projectDir?, lines:[{seq,text}]}
//	PUT  /state/snapshot?id&part&parts&headSha&branch   (octet-stream part)
//	PUT  /state/meta                  {harnessSession?, projectDir?, cwd?, branch?, headSha?}
//	GET  /state/resume                {resume: {from, meta, snapshot} | null}
//	GET  /state/resume/transcript?after=N   {harnessSession, lines, next}
//	GET  /state/resume/snapshot?part=N      (octet-stream part)
package continuity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Config points continuity at its session.
type Config struct {
	// BaseURL is the session's relay base:
	// {api}/v1/organizations/{org}/agents/sessions/{id}.
	BaseURL string
	// Token supplies the session's live bearer for each request (serve wires
	// the heartbeat's refreshed token).
	Token func() string
	HTTP  *http.Client
	Log   io.Writer
	// Workdir is the grounded checkout (the harness's cwd).
	Workdir string
	// ClaudeDir is Claude Code's config directory: $CLAUDE_CONFIG_DIR, else
	// ~/.claude.
	ClaudeDir string
	// FlushEvery is the transcript batch cadence (default 1s).
	FlushEvery time.Duration
	// SnapshotEvery is how often a dirty working copy is snapshotted between
	// turns (default 2m).
	SnapshotEvery time.Duration
}

// ClaudeDirFromEnv is Claude Code's config directory as the harness sees it.
func ClaudeDirFromEnv(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".claude")
	}
	return ".claude"
}

// ProjectDir is the directory Claude Code files a cwd's transcripts under:
// the absolute path with every character that is not a letter or digit
// replaced by '-' (verified against Claude Code 2.1.316: `--resume <id>`
// finds projects/<this>/<id>.jsonl and appends to it under the same id).
func ProjectDir(cwd string) string {
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	return nonAlnum.ReplaceAllString(cwd, "-")
}

var (
	nonAlnum      = regexp.MustCompile(`[^a-zA-Z0-9]`)
	harnessIDRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	projectDirRe  = regexp.MustCompile(`^[A-Za-z0-9-]{1,512}$`)
	errNoEnvelope = fmt.Errorf("continuity: response is not a data envelope")
)

func (c Config) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Config) logf(format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, format, args...)
	}
}

func (c Config) flushEvery() time.Duration {
	if c.FlushEvery > 0 {
		return c.FlushEvery
	}
	return time.Second
}

func (c Config) snapshotEvery() time.Duration {
	if c.SnapshotEvery > 0 {
		return c.SnapshotEvery
	}
	return 2 * time.Minute
}

// request sends one call. A JSON body is marshaled; a []byte body is sent as
// octet-stream. out, when set, receives the envelope's `data`.
func (c Config) request(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
		ctype = "application/octet-stream"
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
		ctype = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.Token != nil {
		if tok := c.Token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("continuity: %s %s answered %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Data == nil {
		return errNoEnvelope
	}
	return json.Unmarshal(env.Data, out)
}

// fetchPart reads one raw snapshot part.
func (c Config) fetchPart(ctx context.Context, part int) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/state/resume/snapshot?part=%d", strings.TrimRight(c.BaseURL, "/"), part), nil)
	if err != nil {
		return nil, nil, err
	}
	if c.Token != nil {
		if tok := c.Token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("continuity: snapshot part %d answered %d", part, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, snapshotPartBytes+1))
	if err != nil {
		return nil, nil, err
	}
	return data, resp.Header, nil
}

// Meta is the resume metadata the control plane keeps per session.
type Meta struct {
	HarnessSession  string `json:"harnessSession,omitempty"`
	ProjectDir      string `json:"projectDir,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	Branch          string `json:"branch,omitempty"`
	HeadSha         string `json:"headSha,omitempty"`
	TranscriptLines *int   `json:"transcriptLines,omitempty"`
}

// PutMeta merges metadata into the session's own state. Best-effort.
func PutMeta(ctx context.Context, cfg Config, m Meta) {
	if err := cfg.request(ctx, http.MethodPut, "/state/meta", m, nil); err != nil {
		cfg.logf("orun agent serve: continuity — could not record session metadata (%v)\n", err)
	}
}
