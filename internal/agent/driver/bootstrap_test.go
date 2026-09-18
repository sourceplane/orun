package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The bootstrap driver (orun-bootstrap-engine BE-O14).
//
// These drive a REAL child process — `sh -c` scripts standing in for the
// engine — because everything that is easy to get wrong here is about a
// process: a stream that ends, a pipe that fills, an exit status, a channel
// nobody reads. A fake that handed the driver a slice of lines would test the
// translation and none of that.
//
// The claims, in order of what they cost when wrong:
//
//  1. the exit status decides the verdict, not the stream. A build that dies
//     mid-phase must not report "completed" because nothing said otherwise.
//  2. nothing is dropped. Unrecognised lines and stderr both reach the
//     transcript — the engine is not the only thing writing to those pipes.
//  3. it never wedges. A build that blocked its own supervisor would be the
//     worst possible failure of "no model attached".

// script runs a shell script as the "engine" and collects everything the
// driver emits. Returns the events in order and the Proc's error.
func script(t *testing.T, sh string, timeout time.Duration) ([]Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	events := make(chan Event, 512)
	steer := make(chan Message)
	approve := make(chan Verdict)
	interrupt := make(chan struct{})

	d := &Bootstrap{Command: "sh", Args: []string{"-c", sh}}
	p, err := d.Launch(ctx, Brief{ID: "b1"}, IO{
		Events: events, Steer: steer, Approve: approve, Interrupt: interrupt,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	collected := make([]Event, 0, 32)
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for e := range events {
			collected = append(collected, e)
		}
	}()

	waitErr := p.Wait()
	close(events)
	<-collectDone
	return collected, waitErr
}

func kinds(events []Event) []EventKind {
	out := make([]EventKind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

func last(events []Event) Event {
	if len(events) == 0 {
		return Event{}
	}
	return events[len(events)-1]
}

func textOf(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e.Text)
		b.WriteString("\n")
	}
	return b.String()
}

const eventLine = `{"schema":"bootstrap-event/v1","runId":"r1","seq":%d,"at":"2026-09-15T10:00:00Z",` +
	`"phase":"%s","state":"%s","narration":"%s"}`

func TestBootstrapNarratesPhasesAndTheWordsBesideThem(t *testing.T) {
	sh := `
echo '{"schema":"bootstrap-event/v1","seq":1,"phase":"01-scaffold","state":"started","narration":"Placing the repository."}'
echo '{"schema":"bootstrap-event/v1","seq":2,"phase":"01-scaffold","state":"done"}'
`
	events, err := script(t, sh, 20*time.Second)
	if err != nil {
		t.Fatalf("a clean build reported an error: %v", err)
	}
	// A phase marker is a HARNESS event; the authored sentence is a MESSAGE.
	// An event with both produces both, marker first — so the marker is on
	// record even when nobody authored words for it.
	if got := kinds(events); len(got) < 4 {
		t.Fatalf("expected marker+message per event, got %v", got)
	}
	if events[0].Kind != EventHarness || events[1].Kind != EventMessage {
		t.Fatalf("expected harness then message, got %v", kinds(events))
	}
	if events[1].Text != "Placing the repository." {
		t.Fatalf("narration lost: %q", events[1].Text)
	}
	// The wordless one still leaves a marker.
	if events[2].Kind != EventHarness || !strings.Contains(events[2].Text, "01-scaffold done") {
		t.Fatalf("a wordless event left no marker: %+v", events[2])
	}
	if events[2].Fields["phase"] != "01-scaffold" || events[2].Fields["state"] != "done" {
		t.Fatalf("phase/state lost from fields: %+v", events[2].Fields)
	}
}

// A failure is an ERROR whatever it was authored to say. The transcript's
// reader is deciding whether to keep waiting, and a failure rendered as an
// ordinary message is one they scroll past.
func TestBootstrapRaisesAFailedPhaseAsAnError(t *testing.T) {
	sh := `echo '{"schema":"bootstrap-event/v1","seq":1,"phase":"03-infrastructure","state":"failed","detail":"Error: 403"}'`
	events, _ := script(t, sh, 20*time.Second)
	var sawError bool
	for _, e := range events {
		if e.Kind == EventError && e.Fields["detail"] == "Error: 403" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("a failed phase did not raise an error: %v", kinds(events))
	}
}

// ── THE CLAIM THAT COSTS THE MOST WHEN WRONG ────────────────────────────────
//
// The child's exit status is the whole verdict. A driver that inferred success
// from the event stream would call a build that died mid-phase "completed"
// because nothing in the stream said otherwise — and the console would show a
// finished build over a half-written product.
func TestBootstrapVerdictComesFromTheExitStatusNotTheStream(t *testing.T) {
	// A stream that says nothing but `done`, and a process that fails anyway.
	sh := `
echo '{"schema":"bootstrap-event/v1","seq":1,"phase":"08-docs","state":"done","narration":"All finished."}'
exit 7
`
	events, waitErr := script(t, sh, 20*time.Second)
	if waitErr == nil {
		t.Fatal("Wait reported success for a child that exited 7")
	}
	end := last(events)
	if end.Kind != EventDone {
		t.Fatalf("the last event was not terminal: %+v", end)
	}
	if end.Fields["status"] != "failed" {
		t.Fatalf("a build that exited 7 reported %q", end.Fields["status"])
	}
}

func TestBootstrapReportsCompletedWhenTheChildSucceeds(t *testing.T) {
	events, err := script(t, `echo '{"schema":"bootstrap-event/v1","seq":1,"state":"done"}'`, 20*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if last(events).Fields["status"] != "completed" {
		t.Fatalf("a clean build reported %+v", last(events).Fields)
	}
}

// The engine is not the only thing writing to these pipes — a hook's own
// output arrives here too — and a driver that showed only what it recognised
// would hide the half of a build that is somebody else's script.
func TestBootstrapPassesThroughWhatItDoesNotRecognise(t *testing.T) {
	sh := `
echo 'terraform apply -auto-approve'
echo '{"not":"an event"}'
echo 'Apply complete! Resources: 12 added.'
echo 'a diagnostic' >&2
`
	events, err := script(t, sh, 20*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	all := textOf(events)
	for _, want := range []string{
		"terraform apply -auto-approve",
		`{"not":"an event"}`,
		"Apply complete! Resources: 12 added.",
		"a diagnostic", // stderr reaches the transcript too
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("dropped %q from the transcript:\n%s", want, all)
		}
	}
}

// A build has no turns to interrupt and no prompt to steer. The channels are
// drained anyway — a runtime sending into an unread channel blocks — and the
// person typing is told, rather than ignored.
func TestBootstrapAnswersSteeringInsteadOfWedging(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	events := make(chan Event, 64)
	steer := make(chan Message)
	interrupt := make(chan struct{})
	d := &Bootstrap{Command: "sh", Args: []string{"-c", "sleep 0.4; echo done-ish"}}
	p, err := d.Launch(ctx, Brief{}, IO{
		Events: events, Steer: steer, Approve: make(chan Verdict), Interrupt: interrupt,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Both sends must complete: an undrained channel here wedges the runtime.
	select {
	case steer <- Message{Text: "do it differently"}:
	case <-time.After(3 * time.Second):
		t.Fatal("Steer blocked: the driver is not draining its inputs")
	}
	select {
	case interrupt <- struct{}{}:
	case <-time.After(3 * time.Second):
		t.Fatal("Interrupt blocked: the driver is not draining its inputs")
	}

	var said strings.Builder
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for e := range events {
			said.WriteString(e.Text)
			said.WriteString("\n")
		}
	}()
	_ = p.Wait()
	close(events)
	<-collected

	if !strings.Contains(said.String(), "not a conversation") {
		t.Fatalf("a steer was swallowed rather than answered:\n%s", said.String())
	}
	if !strings.Contains(said.String(), "cannot be interrupted") {
		t.Fatalf("an interrupt was swallowed rather than answered:\n%s", said.String())
	}
}

// A terraform plan on one line is well past bufio's 64KB default, and a
// scanner that gave up mid-build would take the rest of the transcript with it.
//
// 200KB, not 40KB. The first version of this test built a 40,000-character
// line — comfortably UNDER the 64KB default — so it passed with the enlarged
// buffer removed, which is to say it never reached the property it is named
// for. Found by mutation.
func TestBootstrapSurvivesAVeryLongLine(t *testing.T) {
	sh := `head -c 200000 /dev/zero | tr '\0' 'x'; echo; echo "after-the-long-line"`
	events, err := script(t, sh, 30*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(textOf(events), "after-the-long-line") {
		t.Fatal("the scanner died on a long line and lost everything after it")
	}
}

func TestBootstrapArgsNeedTheControlPlanesEnvironment(t *testing.T) {
	env := map[string]string{envBaselineID: "cirrus@baseline-v6", envBaselineOut: "/work/product"}
	get := func(k string) string { return env[k] }

	args, err := bootstrapArgs(get)
	if err != nil {
		t.Fatalf("bootstrapArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"baseline new cirrus@baseline-v6", "--local", "--out /work/product",
		// Neither of these is optional: a runner restarted on the same tree
		// must not re-place what is there, and a bootstrap that placed files
		// and ran nothing is not a bootstrap.
		"--run-hooks", "--resume",
		"--progress json",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q from %q", want, joined)
		}
	}
	if strings.Contains(joined, "--values") {
		t.Fatalf("added --values with none set: %q", joined)
	}

	env[envBaselineVals] = "/work/values.json"
	args, _ = bootstrapArgs(get)
	if !strings.Contains(strings.Join(args, " "), "--values /work/values.json") {
		t.Fatalf("values file ignored: %v", args)
	}

	// Half-configured is not configured: a driver that guessed the out dir
	// would place somebody's product wherever it happened to be running.
	for _, missing := range []string{envBaselineID, envBaselineOut} {
		saved := env[missing]
		delete(env, missing)
		if _, err := bootstrapArgs(get); err == nil {
			t.Fatalf("built a command with %s unset", missing)
		}
		env[missing] = saved
	}
}

func TestBootstrapIsRegistrableUnderItsOwnID(t *testing.T) {
	d := &Bootstrap{}
	if d.ID() != "bootstrap" {
		t.Fatalf("id is %q", d.ID())
	}
}

func TestBootstrapRefusesToLaunchWithoutItsEnvironment(t *testing.T) {
	d := &Bootstrap{Env: []string{"PATH=" + os.Getenv("PATH")}}
	_, err := d.Launch(context.Background(), Brief{}, IO{Events: make(chan Event, 1)})
	if err == nil {
		t.Fatal("launched a build with nothing to build")
	}
	if !strings.Contains(err.Error(), envBaselineID) {
		t.Fatalf("the refusal does not name what is missing: %v", err)
	}
}

// os/exec hands a child the LAST value of a duplicated key. The driver read
// the FIRST to build the child's arguments — so a later override (serve
// pointing a grounded build at its clone) changed the environment the child
// ran with, and not the `--out` it was told to place into.
func TestLookupReadsTheValueTheChildWillSee(t *testing.T) {
	env := []string{"ORUN_BASELINE_OUT=/home/daytona/product", "X=1", "ORUN_BASELINE_OUT=/home/daytona/work/newne"}
	if got := lookup(env, envBaselineOut); got != "/home/daytona/work/newne" {
		t.Fatalf("lookup = %q, want the last value", got)
	}
	if got := lookup(env, "ABSENT"); got != "" {
		t.Fatalf("lookup of an absent key = %q", got)
	}
}

// End to end through Launch: the child is told to place into the value it
// will also see in its environment.
func TestBootstrapPlacesIntoTheOutItsEnvironmentEndsWith(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "orun")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"args: $*\"\necho \"env: $ORUN_BASELINE_OUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := make(chan Event, 64)
	d := &Bootstrap{Command: stub, Env: []string{
		"PATH=" + os.Getenv("PATH"),
		envBaselineID + "=cirrus@baseline-v10",
		envBaselineOut + "=/home/daytona/product",
		envBaselineOut + "=/home/daytona/work/newne",
	}}
	p, err := d.Launch(context.Background(), Brief{ID: "b1"}, IO{
		Events: events, Steer: make(chan Message), Approve: make(chan Verdict), Interrupt: make(chan struct{}),
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	var lines []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range events {
			lines = append(lines, e.Text)
		}
	}()
	_ = p.Wait()
	close(events)
	<-done
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "--out /home/daytona/work/newne") {
		t.Fatalf("the child was not told to place into its clone:\n%s", all)
	}
	if !strings.Contains(all, "env: /home/daytona/work/newne") {
		t.Fatalf("the child's environment disagrees with its arguments:\n%s", all)
	}
}
