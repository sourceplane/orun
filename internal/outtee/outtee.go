// Package outtee carries a step's live output sink through a context
// (saas-live-runs LR8). The runner puts a writer on the step's context; an
// executor that captures a process's output writes it there too, as it
// arrives, so a remote log can grow while the step runs instead of landing
// whole at its end. The captured output an executor returns is unchanged:
// the tee is a second reader, never the record.
package outtee

import (
	"context"
	"io"
)

type key struct{}

// With returns ctx carrying w as the step's live output sink. A nil w
// returns ctx unchanged.
func With(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, key{}, w)
}

// From is the step's live output sink, or nil.
func From(ctx context.Context) io.Writer {
	if ctx == nil {
		return nil
	}
	w, _ := ctx.Value(key{}).(io.Writer)
	return w
}

// Capture is the writer a process's stdout and stderr go to: buf, and the
// context's sink when it has one. One value for both, so exec runs at most
// one copying goroutine at a time into it.
func Capture(ctx context.Context, buf io.Writer) io.Writer {
	if w := From(ctx); w != nil {
		return &tee{buf: buf, live: w}
	}
	return buf
}

// tee writes to the record first and the sink after; a sink that fails is
// dropped for the rest of the process, never failing the step.
type tee struct {
	buf    io.Writer
	live   io.Writer
	broken bool
}

func (t *tee) Write(p []byte) (int, error) {
	n, err := t.buf.Write(p)
	if !t.broken && n > 0 {
		if _, lerr := t.live.Write(p[:n]); lerr != nil {
			t.broken = true
		}
	}
	return n, err
}

// Tee is the writer for buf with live reading along, for an executor that
// holds its sink directly rather than on a context.
func Tee(buf, live io.Writer) io.Writer {
	if live == nil {
		return buf
	}
	return &tee{buf: buf, live: live}
}
