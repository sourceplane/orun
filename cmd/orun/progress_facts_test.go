package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/scaffold"
)

// A terminal shows what the build made under the step that made it, and
// keeps a health check and a convergence run to the build's own record
// (orun-cloud saas-build-panel BV3).
func TestAFactPrintsAsARowUnderItsStep(t *testing.T) {
	var out bytes.Buffer
	sink := &progressSink{out: &out, mode: progressAuto}
	ctx := context.Background()
	fact := func(meta map[string]string) {
		sink.Emit(ctx, scaffold.Event{State: scaffold.EventFact, Meta: meta})
	}
	fact(map[string]string{"kind": "repo", "value": "acme/notes"})
	fact(map[string]string{"kind": "pr", "value": "#7", "status": "checks", "label": "Product"})
	fact(map[string]string{"kind": "url", "value": "https://notes-console-stage.acme.workers.dev", "env": "stage"})
	fact(map[string]string{"kind": "url", "value": "https://notes-api-edge-stage.acme.workers.dev/health", "pin": "false"})
	fact(map[string]string{"kind": "run", "value": "77", "pin": "false"})
	fact(map[string]string{"kind": "url", "value": "https://down.dev", "status": "unreachable"})

	want := strings.Join([]string{
		"    repository github.com/acme/notes",
		"    pull request #7 · checks running — Product",
		"    stage https://notes-console-stage.acme.workers.dev",
	}, "\n") + "\n"
	if out.String() != want {
		t.Errorf("rows:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPlainProgressPrintsNoFacts(t *testing.T) {
	var out bytes.Buffer
	sink := &progressSink{out: &out, mode: progressPlain}
	sink.Emit(context.Background(), scaffold.Event{State: scaffold.EventFact, Meta: map[string]string{"kind": "repo", "value": "acme/notes"}})
	if out.Len() != 0 {
		t.Errorf("plain is narration only, got %q", out.String())
	}
}

func TestTheRunsOwnStartAndEndAreForThePlatformNotTheTerminal(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []progressMode{progressAuto, progressPlain, progressVerbose} {
		var out bytes.Buffer
		sink := &progressSink{out: &out, mode: mode}
		sink.Emit(ctx, scaffold.Event{State: scaffold.EventStarted, Narration: "Starting notes."})
		sink.Emit(ctx, scaffold.Event{State: scaffold.EventDone, Narration: "notes is complete."})
		sink.Emit(ctx, scaffold.Event{Step: "epic", State: scaffold.EventDone, Narration: "Opened the epic EP-3."})
		if got := strings.TrimSpace(out.String()); !strings.HasSuffix(got, "Opened the epic EP-3.") || strings.Contains(got, "notes") {
			t.Errorf("%s: %q", mode, got)
		}
	}
	var out bytes.Buffer
	(&progressSink{out: &out, mode: progressJSON}).Emit(ctx, scaffold.Event{State: scaffold.EventDone})
	if !strings.Contains(out.String(), `"state":"done"`) {
		t.Errorf("json is the raw stream, the run's end included: %q", out.String())
	}
}
