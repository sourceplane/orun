package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the fake-claude harness (the os/exec re-exec pattern):
// with FAKE_CLAUDE=1 the test binary becomes a scripted Claude Code CLI —
// reading a scenario file of emit/await directives and speaking stream-JSON
// on stdio. Scenarios live in testdata/claude-*.ndjson; they ARE the
// documented wire protocol the driver implements (design.md §4.5), so a
// mapping change without a scenario change fails here, never silently.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CLAUDE") == "1" {
		runFakeClaude()
		return
	}
	os.Exit(m.Run())
}

func runFakeClaude() {
	f, err := os.Open(os.Getenv("FAKE_CLAUDE_SCENARIO"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-claude:", err)
		os.Exit(2)
	}
	defer f.Close()
	out := bufio.NewWriter(os.Stdout)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64<<10), 4<<20)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var d struct {
			Do       string          `json:"do"`
			Raw      json.RawMessage `json:"raw"`
			Contains string          `json:"contains"`
		}
		if err := json.Unmarshal(line, &d); err != nil {
			fmt.Fprintln(os.Stderr, "fake-claude: bad directive:", err)
			os.Exit(2)
		}
		switch d.Do {
		case "emit":
			var buf bytes.Buffer
			if err := json.Compact(&buf, d.Raw); err != nil {
				fmt.Fprintln(os.Stderr, "fake-claude: bad raw:", err)
				os.Exit(2)
			}
			buf.WriteByte('\n')
			out.Write(buf.Bytes())
			out.Flush()
		case "await":
			found := false
			for in.Scan() {
				if strings.Contains(in.Text(), d.Contains) {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "fake-claude: stdin closed awaiting %q\n", d.Contains)
				os.Exit(3)
			}
		}
	}
	os.Exit(0)
}

// fakeClaude returns a driver wired to the re-exec harness with the given
// scenario file.
func fakeClaude(t *testing.T, scenario string) *ClaudeCode {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", scenario))
	if err != nil {
		t.Fatal(err)
	}
	return &ClaudeCode{
		Binary: os.Args[0],
		Env:    []string{"FAKE_CLAUDE=1", "FAKE_CLAUDE_SCENARIO=" + abs},
	}
}

// launchCollect starts the driver and returns the IO channels plus a
// collector that gathers events until Done (or times out).
type collected struct {
	events []Event
}

func (c *collected) kinds() []EventKind {
	out := make([]EventKind, len(c.events))
	for i, e := range c.events {
		out[i] = e.Kind
	}
	return out
}

func (c *collected) first(k EventKind) (Event, bool) {
	for _, e := range c.events {
		if e.Kind == k {
			return e, true
		}
	}
	return Event{}, false
}

func drive(t *testing.T, d Driver, b Brief, react func(Event, chan<- Message, chan<- Verdict, chan struct{})) *collected {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	events := make(chan Event, 16)
	steer := make(chan Message)
	approve := make(chan Verdict, 4)
	interrupt := make(chan struct{}, 1)
	proc, err := d.Launch(ctx, b, IO{Events: events, Steer: steer, Approve: approve, Interrupt: interrupt})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Wait() }()
	c := &collected{}
	for {
		select {
		case e := <-events:
			c.events = append(c.events, e)
			if react != nil {
				react(e, steer, approve, interrupt)
			}
			if e.Kind == EventDone {
				if err := <-waitErr; err != nil {
					t.Fatalf("wait: %v", err)
				}
				return c
			}
		case err := <-waitErr:
			// Process exited without Done: drain buffered events, then fail
			// unless the scenario intends it.
			for {
				select {
				case e := <-events:
					c.events = append(c.events, e)
					continue
				default:
				}
				break
			}
			if _, sawDone := c.first(EventDone); !sawDone {
				t.Fatalf("harness exited without done (err=%v, events=%v)", err, c.kinds())
			}
			return c
		case <-ctx.Done():
			t.Fatalf("timed out; events so far: %v", c.kinds())
		}
	}
}

func TestClaudeCodeMappingBasic(t *testing.T) {
	d := fakeClaude(t, "claude-basic.ndjson")
	c := drive(t, d, Brief{ID: "sha256:b1", Instructions: "you are the implementer", Task: "ORN-142"}, nil)

	init, ok := c.first(EventHarness)
	if !ok || init.Fields["harnessSession"] != "cc-sess-1" || init.Fields["model"] != "claude-opus-4-8" {
		t.Fatalf("init harness event = %+v", init)
	}
	if _, ok := c.first(EventDelta); !ok {
		t.Fatal("no delta streamed")
	}
	msg, ok := c.first(EventMessage)
	if !ok || msg.Text != "Reading the brief." {
		t.Fatalf("message = %+v", msg)
	}
	tc, ok := c.first(EventToolCall)
	if !ok || tc.Fields["tool"] != "mcp__orun__work_get" || tc.Fields["toolUseId"] != "tu1" {
		t.Fatalf("tool_call = %+v", tc)
	}
	tr, ok := c.first(EventToolResult)
	if !ok || !strings.Contains(tr.Text, "ORN-142") {
		t.Fatalf("tool_result = %+v", tr)
	}
	cost, ok := c.first(EventCost)
	if !ok || cost.Fields["tokens"] != 150 {
		t.Fatalf("cost = %+v", cost)
	}
	done, ok := c.first(EventDone)
	if !ok || done.Fields["status"] != "completed" || done.Fields["harnessSession"] != "cc-sess-1" {
		t.Fatalf("done = %+v", done)
	}
}

func TestClaudeCodePermissionBridge(t *testing.T) {
	d := fakeClaude(t, "claude-permission.ndjson")
	var approvalSeen bool
	c := drive(t, d, Brief{Instructions: "impl"}, func(e Event, _ chan<- Message, approve chan<- Verdict, _ chan struct{}) {
		if e.Kind == EventApproval {
			approvalSeen = true
			if e.Fields["tool"] != "mcp__orun__contract_propose" {
				t.Errorf("approval tool = %v", e.Fields["tool"])
			}
			approve <- Verdict{RequestID: e.RequestID, Approved: true, Reason: "lgtm"}
		}
	})
	if !approvalSeen {
		t.Fatal("no approval request bridged")
	}
	msg, ok := c.first(EventMessage)
	if !ok || msg.Text != "Contract proposed." {
		t.Fatalf("post-approval message = %+v (harness never got the allow)", msg)
	}
}

func TestClaudeCodeDeniedPermission(t *testing.T) {
	d := fakeClaude(t, "claude-permission-denied.ndjson")
	c := drive(t, d, Brief{Instructions: "impl"}, func(e Event, _ chan<- Message, approve chan<- Verdict, _ chan struct{}) {
		if e.Kind == EventApproval {
			approve <- Verdict{RequestID: e.RequestID, Approved: false, Reason: "out of blast radius"}
		}
	})
	msg, ok := c.first(EventMessage)
	if !ok || msg.Text != "Understood, skipping." {
		t.Fatalf("post-denial message = %+v (harness never got the deny)", msg)
	}
}

func TestClaudeCodeSteerAndInterrupt(t *testing.T) {
	d := fakeClaude(t, "claude-steer-interrupt.ndjson")
	steered := false
	c := drive(t, d, Brief{Instructions: "impl"}, func(e Event, steer chan<- Message, _ chan<- Verdict, interrupt chan struct{}) {
		if e.Kind == EventMessage && e.Text == "Working." && !steered {
			steered = true
			go func() { steer <- Message{Text: "also update the changelog"} }()
		}
		if e.Kind == EventMessage && e.Text == "Updating changelog too." {
			interrupt <- struct{}{}
		}
	})
	if _, ok := c.first(EventDone); !ok {
		t.Fatalf("no done after interrupt; %v", c.kinds())
	}
}

func TestClaudeCodeConformance(t *testing.T) {
	d := fakeClaude(t, "claude-basic.ndjson")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rep := CheckConformance(ctx, d, Brief{ID: "sha256:b1", Instructions: "impl", Task: "ORN-142"})
	if !rep.OK() {
		t.Fatalf("conformance: %s", rep)
	}
	if !rep.SawDone {
		t.Fatal("conformance run saw no done")
	}
}

func TestClaudeCodeGarbageLineIsAnError(t *testing.T) {
	d := fakeClaude(t, "claude-garbage.ndjson")
	c := drive(t, d, Brief{Instructions: "impl"}, nil)
	if _, ok := c.first(EventError); !ok {
		t.Fatalf("garbage line not surfaced: %v", c.kinds())
	}
	if _, ok := c.first(EventDone); !ok {
		t.Fatal("stream did not recover after garbage line")
	}
}

// TestClaudeCodeLiveSmoke exercises the real binary end to end. Gated: it
// needs `claude` on PATH and a model credential in the environment.
func TestClaudeCodeLiveSmoke(t *testing.T) {
	if os.Getenv("ORUN_LIVE_DRIVER_SMOKE") != "1" {
		t.Skip("set ORUN_LIVE_DRIVER_SMOKE=1 to run against the real claude binary")
	}
	d := &ClaudeCode{ExtraArgs: []string{"--max-turns", "2"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	events := make(chan Event, 64)
	proc, err := d.Launch(ctx, Brief{
		Instructions: "You are a smoke test. Reply with the single word: ok.",
		Workdir:      t.TempDir(),
	}, IO{Events: events, Steer: make(chan Message), Approve: make(chan Verdict, 1), Interrupt: make(chan struct{}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	go proc.Wait()
	for e := range events {
		t.Logf("%s %q %v", e.Kind, e.Text, e.Fields)
		if e.Kind == EventDone {
			return
		}
	}
}
