package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// The BOOTSTRAP driver (orun-bootstrap-engine BE-O14): a build is the thing
// being supervised, and no model is attached.
//
// # Why a driver and not an entrypoint
//
// The obvious reading of "the hosted runner" is to replace `orun agent serve`
// in the sandbox with the build command. That loses everything serve provides:
// the heartbeat, token rotation, the attach plane, sealing, and the terminal
// state the console reads to tell a finished build from a hung one. Every one
// of those would have to be rebuilt in a wrapper, which is a second
// implementation of things the binary already does correctly.
//
// So serve still supervises. What changes is what it supervises: `claudecode`
// drives a model, `bootstrap` drives a build. The seam was already the right
// shape — "a coding agent (Claude Code first, any binary next) is an executor
// behind a narrow interface" — and a build is just another binary.
//
// # The two streams, and why this driver does not send one of them
//
// A build produces two records, and they are not the same thing:
//
//	the TRANSCRIPT   what a person watching the session sees. That is this
//	                 driver's job, translated into the closed event vocabulary.
//	the BUILD STREAM `bootstrap-event/v1`, keyed (org, run, seq), which the
//	                 console's build page reads. That is NOT sent from here.
//
// The child process sends it. `orun new` already builds a platform sink from
// the sandbox identity (BE-O13), and the child inherits that environment — so
// the build reports itself, exactly as it does when a human runs it in a
// sandbox. A driver that also posted would be a second sender racing the first
// for the same seq numbers, which the ingest door would refuse.
//
// This driver therefore READS the same `--progress json` lines only to narrate
// them, and sends nothing.
//
// # It is not steerable
//
// A build has no turns to interrupt and no prompt to steer. The channels are
// still drained — a runtime that sends into an unread channel blocks — and
// every message is answered with a line saying so, rather than silently
// swallowed. A person typing into a build's panel deserves to be told it is
// not listening.

// Bootstrap runs a product build and narrates it.
type Bootstrap struct {
	// Command overrides the binary for tests. Empty means this orun.
	Command string
	// Args overrides the whole argument vector for tests. Empty means the
	// vector is built from the environment (see bootstrapArgs).
	Args []string
	// Env overrides the process environment for tests. Nil means inherit.
	Env []string
}

func (b *Bootstrap) ID() string { return "bootstrap" }

// Environment the control plane injects to say WHAT to build. The session
// identity (ORUN_CLOUD_API, ORUN_ORG_ID, ORUN_SESSION_ID, ORUN_SESSION_TOKEN)
// is already there for serve, and the child reads it for its own reporting.
const (
	envBaselineID   = "ORUN_BASELINE_ID"     // `cirrus` or `cirrus@baseline-v6`
	envBaselineOut  = "ORUN_BASELINE_OUT"    // where to place the product
	envBaselineVals = "ORUN_BASELINE_VALUES" // path to a --values JSON file
)

// bootstrapArgs builds the command from the environment.
//
// `--resume` is not optional and not a flag the caller chooses: a runner whose
// sandbox was reclaimed mid-build is restarted on the same working tree, and
// without it the second attempt would re-place phases that are already there.
// `--run-hooks` likewise — a bootstrap that placed files and ran nothing is
// not a bootstrap.
func bootstrapArgs(get func(string) string) ([]string, error) {
	id := strings.TrimSpace(get(envBaselineID))
	out := strings.TrimSpace(get(envBaselineOut))
	if id == "" || out == "" {
		return nil, fmt.Errorf(
			"driver bootstrap: %s and %s must be set (the control plane injects them)",
			envBaselineID, envBaselineOut,
		)
	}
	args := []string{"baseline", "new", id, "--local", "--out", out,
		"--run-hooks", "--resume", "--progress", "json"}
	if vals := strings.TrimSpace(get(envBaselineVals)); vals != "" {
		args = append(args, "--values", vals)
	}
	return args, nil
}

func (b *Bootstrap) Launch(ctx context.Context, brief Brief, bio IO) (Proc, error) {
	get := os.Getenv
	if b.Env != nil {
		get = func(k string) string { return lookup(b.Env, k) }
	}
	args := b.Args
	if args == nil {
		built, err := bootstrapArgs(get)
		if err != nil {
			return nil, err
		}
		args = built
	}
	bin := b.Command
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			// A build that cannot find its own binary is not a build; saying so
			// here beats a child process that fails for a reason nobody can see.
			return nil, fmt.Errorf("driver bootstrap: cannot resolve the orun binary: %w", err)
		}
		bin = exe
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	if brief.Workdir != "" {
		cmd.Dir = brief.Workdir
	}
	if b.Env != nil {
		cmd.Env = b.Env
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("driver bootstrap: stdout: %w", err)
	}
	// Merged deliberately: the engine writes its events to stdout and its
	// diagnostics to stderr, and a build whose failure is only on stderr must
	// still have that failure reach the person watching.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("driver bootstrap: stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("driver bootstrap: starting %s: %w", bin, err)
	}

	p := &bootstrapProc{done: make(chan struct{})}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); narrate(ctx, stdout, bio.Events) }()
	go func() { defer wg.Done(); relayStderr(ctx, stderr, bio.Events) }()
	// Drain the input channels for the life of the run: a runtime sending into
	// an unread channel blocks, and a build that wedged its own supervisor
	// would be the worst possible failure of "no model attached".
	//
	// STOPPED BEFORE Wait RETURNS, and joined rather than merely signalled. The
	// runtime closes the events channel once the driver is done, and a drainer
	// still running then would send on a closed channel and panic — taking down
	// the supervisor of a build that had just succeeded. Found by `-race`.
	stopInputs := make(chan struct{})
	var inputs sync.WaitGroup
	inputs.Add(1)
	go func() { defer inputs.Done(); drainInputs(ctx, stopInputs, bio) }()

	go func() {
		defer close(p.done)
		wg.Wait()
		err := cmd.Wait()
		p.err = err
		close(stopInputs)
		inputs.Wait()
		// Last, and after the drainer has finished: the terminal event is what
		// the runtime seals on, so nothing may follow it.
		send(ctx, bio.Events, doneEvent(err))
	}()
	return p, nil
}

// doneEvent is the terminal event, and the only place this driver decides
// whether a build succeeded. The child's exit status is the whole answer:
// the engine exits non-zero when a phase fails, and a driver that inferred
// success from the event stream would call a build that died mid-phase
// "completed" because nothing said otherwise.
func doneEvent(err error) Event {
	if err != nil {
		return Event{
			Kind:   EventDone,
			Text:   "the build did not finish: " + err.Error(),
			Fields: map[string]any{"status": "failed", "error": err.Error()},
		}
	}
	return Event{Kind: EventDone, Text: "the build finished", Fields: map[string]any{"status": "completed"}}
}

// bootstrapEvent is `bootstrap-event/v1` as this driver reads it. A subset:
// the driver narrates, it does not re-send, so it needs only what a reader
// would want to see.
type bootstrapEvent struct {
	Schema    string            `json:"schema"`
	Seq       int               `json:"seq"`
	Phase     string            `json:"phase"`
	Step      string            `json:"step"`
	State     string            `json:"state"`
	Narration string            `json:"narration"`
	Detail    string            `json:"detail"`
	Meta      map[string]string `json:"meta"`
}

// narrate turns the engine's stream into the runtime's vocabulary.
//
// A line that is not a `bootstrap-event/v1` object is passed through as a
// message rather than dropped. The engine is not the only thing that writes to
// this pipe — a hook's own output arrives here too — and a driver that showed
// only what it recognised would hide the half of a build that is somebody
// else's script.
func narrate(ctx context.Context, r io.Reader, out chan<- Event) {
	// WHATEVER HAPPENS, KEEP READING. A scanner that stops — a line past the
	// buffer, a read error — leaves the child writing into a pipe nobody
	// drains, and a full pipe blocks it forever: the build does not fail, it
	// HANGS, holding its sandbox until the TTL reclaims it. Found by mutation:
	// removing the enlarged buffer below turned a test failure into a timeout.
	defer func() { _, _ = io.Copy(io.Discard, r) }()
	sc := bufio.NewScanner(r)
	// A terraform plan on one line is well past the 64KB default.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e bootstrapEvent
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &e) != nil || e.Schema == "" {
			send(ctx, out, Event{Kind: EventMessage, Text: line})
			continue
		}
		for _, ev := range translate(e) {
			send(ctx, out, ev)
		}
	}
}

// translate maps one engine event onto the closed vocabulary.
//
// The phase transition is a HARNESS event, which `driver.go` defines as
// "harness metadata worth keeping ... phase markers" — precisely this. The
// narration is a MESSAGE, because it is the sentence a person reads. An event
// carrying both produces both, in that order, so the marker is on record even
// when nobody authored words for it.
func translate(e bootstrapEvent) []Event {
	fields := map[string]any{"phase": e.Phase, "state": e.State, "seq": e.Seq}
	if e.Step != "" {
		fields["step"] = e.Step
	}
	if e.Detail != "" {
		fields["detail"] = e.Detail
	}
	for k, v := range e.Meta {
		fields["meta."+k] = v
	}

	// A failure is an ERROR, whatever it was authored to say. The transcript's
	// reader is deciding whether to keep waiting, and a failure rendered as an
	// ordinary message is one they scroll past.
	kind := EventHarness
	text := phaseLine(e)
	if e.State == "failed" {
		kind = EventError
	}
	events := []Event{{Kind: kind, Text: text, Fields: fields}}
	if n := strings.TrimSpace(e.Narration); n != "" {
		events = append(events, Event{Kind: EventMessage, Text: n})
	}
	return events
}

// phaseLine is the generated marker: never silence, and never a claim the
// engine did not make. `events.go` holds the same rule for narration.
func phaseLine(e bootstrapEvent) string {
	switch {
	case e.Phase == "" && e.Step == "":
		return "build " + e.State
	case e.Step == "":
		return e.Phase + " " + e.State
	default:
		return e.Phase + " " + e.Step + " " + e.State
	}
}

// relayStderr surfaces the child's diagnostics. Not errors by themselves — the
// engine logs plenty that is merely informative — so they arrive as messages,
// and the exit status decides the verdict.
func relayStderr(ctx context.Context, r io.Reader, out chan<- Event) {
	defer func() { _, _ = io.Copy(io.Discard, r) }() // as above: never stop reading
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			send(ctx, out, Event{Kind: EventMessage, Text: line})
		}
	}
}

// drainInputs answers steering rather than swallowing it. A build has no turns
// to interrupt and no prompt to steer, and a person typing into its panel
// should be told that, not ignored.
func drainInputs(ctx context.Context, stop <-chan struct{}, bio IO) {
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-bio.Steer:
			send(ctx, bio.Events, Event{
				Kind: EventMessage,
				Text: "This is a build, not a conversation — it runs the baseline's own phases and takes no instructions. Cancel it if you need it to stop.",
			})
		case v := <-bio.Approve:
			// Nothing here requests approval, so a verdict is a stray. Logged
			// as one rather than dropped: a verdict arriving for a request that
			// was never made means two sessions are crossed somewhere.
			send(ctx, bio.Events, Event{
				Kind:   EventMessage,
				Text:   "ignoring a verdict this build never asked for",
				Fields: map[string]any{"requestId": v.RequestID},
			})
		case <-bio.Interrupt:
			send(ctx, bio.Events, Event{
				Kind: EventMessage,
				Text: "A build cannot be interrupted mid-phase; cancel the session to stop it.",
			})
		}
	}
}

// send never blocks past cancellation: a driver that wedged on a full channel
// would hold the build open with nobody reading it.
func send(ctx context.Context, out chan<- Event, e Event) {
	select {
	case out <- e:
	case <-ctx.Done():
	}
}

// lookup reads a key from an environment slice the way os/exec will hand it
// to the child: the LAST value of a duplicated key wins. Reading the first
// built the arguments from one value while the child ran with another.
func lookup(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if name, v, ok := strings.Cut(kv, "="); ok && name == key {
			value = v
		}
	}
	return value
}

type bootstrapProc struct {
	done chan struct{}
	err  error
}

func (p *bootstrapProc) Wait() error {
	<-p.done
	return p.err
}
