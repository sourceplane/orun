package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// IsGitHubActions reports whether the current process is executing inside a
// GitHub Actions runner. The runner sets GITHUB_ACTIONS=true for every step.
func IsGitHubActions() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true")
}

// GHARenderer emits GitHub Actions-aware output: collapsible log groups,
// workflow-command annotations, and per-job output buffering so that
// concurrently executing jobs render as clean, non-interleaved sections in
// the GitHub Actions log viewer.
//
// Lifecycle:
//   - JobBuffer(jobID) returns a per-job sink that callers write into freely.
//   - Group(buf, title, fn) wraps fn's writes in ::group::/::endgroup::.
//   - FlushJob(jobID) atomically copies the buffered job output to the
//     underlying writer, guaranteeing groups never interleave across jobs.
//
// Color/ANSI is intentionally disabled inside groups: the GHA log viewer
// renders ANSI but it is noisier than necessary for CI archives.
type GHARenderer struct {
	w        io.Writer
	mu       sync.Mutex
	bufs     map[string]*GHAJobBuffer
	commands bool
}

// NewGHARenderer constructs a renderer that writes to w. Pass an os.Stdout-like
// writer; the renderer takes a write lock around every Flush so callers can
// share the underlying stream safely.
func NewGHARenderer(w io.Writer) *GHARenderer {
	return &GHARenderer{w: w, bufs: map[string]*GHAJobBuffer{}, commands: true}
}

// JobBuffer returns the per-job output buffer, lazily creating it on first
// access. Buffers are isolated per jobID so concurrent jobs do not intermix
// lines until the explicit FlushJob call.
func (g *GHARenderer) JobBuffer(jobID string) *GHAJobBuffer {
	g.mu.Lock()
	defer g.mu.Unlock()
	if buf, ok := g.bufs[jobID]; ok {
		return buf
	}
	buf := &GHAJobBuffer{jobID: jobID}
	g.bufs[jobID] = buf
	return buf
}

// FlushJob writes the job buffer's accumulated content to the underlying
// writer in a single locked operation, then drops the buffer. Safe to call
// from concurrent goroutines.
func (g *GHARenderer) FlushJob(jobID string) {
	g.mu.Lock()
	buf, ok := g.bufs[jobID]
	if ok {
		delete(g.bufs, jobID)
	}
	g.mu.Unlock()
	if !ok || buf == nil {
		return
	}
	buf.mu.Lock()
	data := buf.buf.Bytes()
	buf.mu.Unlock()
	if len(data) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.w.Write(data)
	if !bytes.HasSuffix(data, []byte{'\n'}) {
		fmt.Fprintln(g.w)
	}
}

// Print writes a top-level (non-grouped) line directly to the underlying
// writer, locked so it cannot tear with concurrent FlushJob calls.
func (g *GHARenderer) Print(line string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintln(g.w, line)
}

// PrintBlock writes multiple top-level lines atomically.
func (g *GHARenderer) PrintBlock(lines []string) {
	if len(lines) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, ln := range lines {
		fmt.Fprintln(g.w, ln)
	}
}

// Notice/Warning/Error emit GHA workflow-command annotations that surface in
// the PR check summary and the file-based annotation panel.
func (g *GHARenderer) Notice(msg string)  { g.command("notice", nil, msg) }
func (g *GHARenderer) Warning(msg string) { g.command("warning", nil, msg) }
func (g *GHARenderer) Error(msg string)   { g.command("error", nil, msg) }

func (g *GHARenderer) command(name string, props map[string]string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	header := name
	if len(props) > 0 {
		parts := make([]string, 0, len(props))
		for k, v := range props {
			parts = append(parts, k+"="+escapeProperty(v))
		}
		header = name + " " + strings.Join(parts, ",")
	}
	g.Print(fmt.Sprintf("::%s::%s", header, escapeData(value)))
}

// GHAJobBuffer collects output for a single job. It implements io.Writer so
// it can be plugged into existing Fprintf-style call sites unchanged.
type GHAJobBuffer struct {
	jobID string
	mu    sync.Mutex
	buf   bytes.Buffer
	depth int
}

func (b *GHAJobBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Println appends a line to the buffer.
func (b *GHAJobBuffer) Println(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Fprintln(&b.buf, line)
}

// PrintBlock appends multiple lines to the buffer.
func (b *GHAJobBuffer) PrintBlock(lines []string) {
	if len(lines) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ln := range lines {
		fmt.Fprintln(&b.buf, ln)
	}
}

// OpenGroup writes a ::group:: marker into the buffer. GHA does not nest
// groups; a depth counter is tracked so a Step group inside an already-open
// Job context degrades gracefully (the inner group becomes a heading line).
func (b *GHAJobBuffer) OpenGroup(title string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.depth == 0 {
		fmt.Fprintf(&b.buf, "::group::%s\n", escapeData(title))
	} else {
		// Nested: render as a section divider since GHA flattens groups.
		fmt.Fprintf(&b.buf, "\n──▶ %s\n", title)
	}
	b.depth++
}

// CloseGroup writes a matching ::endgroup:: marker.
func (b *GHAJobBuffer) CloseGroup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.depth <= 0 {
		return
	}
	b.depth--
	if b.depth == 0 {
		fmt.Fprintln(&b.buf, "::endgroup::")
	}
}

// Annotation writes a workflow-command annotation into the buffer so it is
// flushed in-order with the surrounding job logs.
func (b *GHAJobBuffer) Annotation(level, msg string, props map[string]string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	header := level
	if len(props) > 0 {
		parts := make([]string, 0, len(props))
		for k, v := range props {
			parts = append(parts, k+"="+escapeProperty(v))
		}
		header = level + " " + strings.Join(parts, ",")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Fprintf(&b.buf, "::%s::%s\n", header, escapeData(msg))
}

// escapeData / escapeProperty implement the GHA workflow-command escaping
// rules so that arbitrary user content cannot break out of an annotation.
//
// See: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions
func escapeData(s string) string {
	r := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return r.Replace(s)
}

func escapeProperty(s string) string {
	r := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	return r.Replace(s)
}
