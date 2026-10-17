package gha

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/outtee"
)

// saas-live-runs LR8: what a live sink receives is the step's recorded
// output, line for line — whatever sizes the process's writes come in.
func TestCommandStreamIsTheRecordedOutput(t *testing.T) {
	cases := []string{
		"",
		"one line",
		"a\nb\n",
		"a\n\n\nb\n\n",
		"::add-mask::hunter2-secret\nthe token is hunter2-secret\ndone\n",
		"before hunter2-secret\n::add-mask::hunter2-secret\nafter hunter2-secret",
		"::group::Install\npnpm i\n::endgroup::\n::warning file=x::careful\n::set-output name=a::1\n",
		"::debug::noise\n\n::notice::hello\r\nprogress\r50%\r100%\n",
		"tail without newline",
		"x\n\n::endgroup::",
	}
	masks := []string{"pre-registered"}
	for _, raw := range cases {
		raw := raw + "\nalso pre-registered here\n"
		want := strings.TrimRight(processWorkflowCommands(strings.TrimRight(raw, "\n"), masks).Output, "\n")
		for _, size := range []int{1, 3, 7, 64, 1 << 20} {
			var got bytes.Buffer
			live := liveCommands(outtee.With(context.Background(), &got), masks)
			for i := 0; i < len(raw); i += size {
				end := i + size
				if end > len(raw) {
					end = len(raw)
				}
				_, _ = live.Write([]byte(raw[i:end]))
			}
			live.Close()
			if strings.TrimRight(got.String(), "\n") != want {
				t.Fatalf("raw %q in %d-byte writes:\n got %q\nwant %q", raw, size, got.String(), want)
			}
			// Masks apply from the line after add-mask on, as in the record:
			// the command itself and every later mention never reach the sink.
			if strings.Contains(got.String(), "add-mask") || strings.Contains(got.String(), "pre-registered") ||
				strings.Contains(got.String(), "token is hunter2") || strings.Contains(got.String(), "after hunter2") {
				t.Fatalf("a masked value reached the sink: %q", got.String())
			}
		}
	}
}

func TestNoSinkNoStream(t *testing.T) {
	live := liveCommands(context.Background(), nil)
	if live != nil || live.sink() != nil {
		t.Fatal("no sink should mean no stream")
	}
	live.Close() // nil-safe
}

func TestRunProcessTeeStreamsAndReturnsTheSameOutput(t *testing.T) {
	var live bytes.Buffer
	out, err := runProcessTee(context.Background(), &live, "sh", []string{"-c", "echo one; echo two 1>&2; printf three"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "one\ntwo\nthree" || live.String() != "one\ntwo\nthree" {
		t.Fatalf("out %q live %q", out, live.String())
	}
}
