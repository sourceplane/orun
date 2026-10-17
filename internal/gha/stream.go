package gha

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/sourceplane/orun/internal/outtee"
)

// commandStream is processWorkflowCommands applied to a process's output as
// it arrives (saas-live-runs LR8): each complete line goes through the same
// sanitizeWorkflowLine, against the masks the step started with plus every
// add-mask seen so far, and what the log shows is written to dst. So a live
// sink never sees an add-mask value, a masked value or a suppressed command,
// and what it receives is the step's recorded output, line for line.
//
// Blank lines are held until a line with text follows: the recorded output
// is trimmed of trailing newlines, so the stream drops trailing blank lines
// too. A sink that fails is dropped for the rest of the process.
type commandStream struct {
	dst     io.Writer
	masks   []string
	scratch workflowCommandResult
	pending []byte
	blanks  int
	broken  bool
}

// liveCommands is the context's live sink behind the workflow-command
// filter, or nil when the step has no sink.
func liveCommands(ctx context.Context, masks []string) *commandStream {
	w := outtee.From(ctx)
	if w == nil {
		return nil
	}
	return &commandStream{dst: w, masks: append([]string{}, masks...), scratch: newWorkflowCommandResult()}
}

// sink is the stream as a writer, or a nil interface for a nil stream.
func (c *commandStream) sink() io.Writer {
	if c == nil {
		return nil
	}
	return c
}

func (c *commandStream) Write(p []byte) (int, error) {
	c.pending = append(c.pending, p...)
	for {
		i := bytes.IndexByte(c.pending, '\n')
		if i < 0 {
			break
		}
		c.line(string(c.pending[:i]))
		c.pending = c.pending[i+1:]
	}
	return len(p), nil
}

// Close writes the last line (the one with no newline after it) and drops
// held blank lines. Nil-safe.
func (c *commandStream) Close() {
	if c == nil {
		return
	}
	if len(c.pending) > 0 {
		c.line(string(c.pending))
		c.pending = nil
	}
	c.blanks = 0
}

func (c *commandStream) line(l string) {
	out, keep := sanitizeWorkflowLine(l, &c.masks, &c.scratch)
	if !keep {
		return
	}
	if out == "" {
		c.blanks++
		return
	}
	var b strings.Builder
	for ; c.blanks > 0; c.blanks-- {
		b.WriteByte('\n')
	}
	b.WriteString(out)
	b.WriteByte('\n')
	if c.broken {
		return
	}
	if _, err := io.WriteString(c.dst, b.String()); err != nil {
		c.broken = true
	}
}
