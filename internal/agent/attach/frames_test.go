package attach

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden fixture files")

// fixtureSequences are the shared cross-repo protocol contract
// (attach-protocol.md §7): each sequence documents one lifecycle. The files
// under testdata/ are vendored verbatim into orun-cloud
// (packages/contracts/src/agents-attach/fixtures/) and both codecs must
// round-trip them byte-identically.
func fixtureSequences() map[string][]Frame {
	info := SessionInfo{SessionID: "as_fixture01", BriefID: "sha256:brief", AgentType: "implementer",
		Task: "ORN-142", RunKind: "implementation", Harness: "claude-code", Model: "claude-opus-4-8"}
	return map[string][]Frame{
		"attach-replay-live.ndjson": {
			AttachFrame(-1, "tui"),
			HelloFrame(info, "running", 3),
			EventFrame(0, "state_changed", "", map[string]any{"state": "running"}, ""),
			EventFrame(1, "message_agent", "", map[string]any{"text": "reading brief"}, ""),
			EventFrame(2, "tool_call", "", map[string]any{"decision": "allow", "tool": "catalog_affected"}, ""),
			EventFrame(3, "tool_result", "", map[string]any{"text": "affected set resolved"}, "sha256:chunk1"),
			LiveFrame(-1),
			DeltaFrame(2, "imple"),
			DeltaFrame(2, "menting"),
			EventFrame(4, "message_agent", "", map[string]any{"text": "implementing ORN-142"}, ""),
			PresenceFrame([]Head{{Principal: "usr_alice", Surface: "tui"}}),
			PingFrame("2026-07-10T00:00:00Z"),
			PongFrame("2026-07-10T00:00:00Z"),
			ByeFrame("terminal"),
		},
		"steer-and-verdict.ndjson": {
			SteerFrame("in-1", "also update the changelog"),
			AckFrame("in-1", true, ""),
			EventFrame(5, "message_user", "", map[string]any{"principal": "usr_alice", "text": "also update the changelog"}, ""),
			EventFrame(6, "approval_requested", "", map[string]any{"requestId": "req-1", "tool": "contract_propose"}, ""),
			VerdictFrame("in-2", "req-1", true, "lgtm"),
			AckFrame("in-2", true, ""),
			EventFrame(7, "approval_resolved", "", map[string]any{"approved": true, "principal": "usr_alice", "reason": "lgtm", "requestId": "req-1"}, ""),
		},
		"verdict-race.ndjson": {
			EventFrame(6, "approval_requested", "", map[string]any{"requestId": "req-1", "tool": "contract_propose"}, ""),
			VerdictFrame("a-1", "req-1", true, "lgtm"),
			AckFrame("a-1", true, ""),
			VerdictFrame("b-1", "req-1", false, "too risky"),
			AckFrame("b-1", false, ReasonNotPending),
		},
		"interrupt-and-end.ndjson": {
			InterruptFrame("in-3"),
			AckFrame("in-3", true, ""),
			EventFrame(8, "harness_event", "", map[string]any{"phase": "interrupted", "principal": "usr_alice"}, ""),
			EndFrame("in-4"),
			AckFrame("in-4", true, ""),
			EventFrame(9, "harness_event", "", map[string]any{"phase": "end_requested", "principal": "usr_alice"}, ""),
			EventFrame(10, "state_changed", "", map[string]any{"state": "canceled"}, ""),
			ByeFrame("terminal"),
		},
		"resume-from-cursor.ndjson": {
			AttachFrame(5, "console"),
			HelloFrame(info, "running", 8),
			EventFrame(6, "approval_requested", "", map[string]any{"requestId": "req-1", "tool": "contract_propose"}, ""),
			EventFrame(7, "approval_resolved", "", map[string]any{"approved": true, "principal": "usr_alice", "reason": "lgtm", "requestId": "req-1"}, ""),
			EventFrame(8, "cost_sample", "", map[string]any{"tokens": 4812}, ""),
			LiveFrame(5),
			DetachFrame(),
		},
		"errors.ndjson": {
			ErrorFrame(CodeVersion, "unsupported protocol version"),
			ErrorFrame(CodeBadFrame, "not json"),
			AckFrame("in-9", false, ReasonTerminal),
			ByeFrame(ReasonLagged),
		},
	}
}

func TestFixturesGoldenRoundTrip(t *testing.T) {
	for name, frames := range fixtureSequences() {
		path := filepath.Join("testdata", name)
		var buf bytes.Buffer
		for _, f := range frames {
			if err := WriteFrame(&buf, f); err != nil {
				t.Fatalf("%s: write: %v", name, err)
			}
		}
		if *update {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update to generate)", name, err)
		}
		if !bytes.Equal(want, buf.Bytes()) {
			t.Fatalf("%s: fixture drift — encoder output differs from golden file.\nwant:\n%s\ngot:\n%s", name, want, buf.String())
		}
		// Decode every line and re-encode: byte-identical (the cross-repo
		// conformance property both codecs must hold).
		dec := NewDecoder(bytes.NewReader(want))
		var rt bytes.Buffer
		for {
			f, err := dec.Next()
			if err != nil {
				break
			}
			if err := WriteFrame(&rt, f); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(want, rt.Bytes()) {
			t.Fatalf("%s: decode→encode not byte-identical.\nwant:\n%s\ngot:\n%s", name, want, rt.String())
		}
	}
}

func TestDecoderSkipsBlankAndFailsLoudOnGarbage(t *testing.T) {
	dec := NewDecoder(strings.NewReader("\n\n" + `{"v":1,"t":"ping","at":"x"}` + "\n"))
	f, err := dec.Next()
	if err != nil || f.T != TPing {
		t.Fatalf("got %+v, %v", f, err)
	}
	dec = NewDecoder(strings.NewReader("not json\n"))
	if _, err := dec.Next(); err == nil {
		t.Fatal("garbage line must error")
	}
}

func TestUnknownFieldsAndTypesTolerated(t *testing.T) {
	// Forward compatibility: unknown frame type decodes (consumer ignores by
	// T), unknown fields are dropped without error.
	dec := NewDecoder(strings.NewReader(`{"v":1,"t":"future_thing","novel":true}` + "\n"))
	f, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.T != "future_thing" {
		t.Fatalf("t = %q", f.T)
	}
}

func TestFrameJSONShape(t *testing.T) {
	// The wire shape is part of the cross-repo contract: spot-check exact
	// serializations so a struct-tag regression is a loud diff here, not a
	// silent drift discovered in the cloud repo.
	cases := map[string]Frame{
		`{"v":1,"t":"live","fromSeq":-1}`:                                          LiveFrame(-1),
		`{"v":1,"t":"delta","turn":2,"text":"hi"}`:                                 DeltaFrame(2, "hi"),
		`{"v":1,"t":"steer","text":"go","ref":"r1"}`:                               SteerFrame("r1", "go"),
		`{"v":1,"t":"verdict","ref":"r2","requestId":"q1","approved":false}`:       VerdictFrame("r2", "q1", false, ""),
		`{"v":1,"t":"ack","ref":"r2","ok":false,"reason":"not_pending"}`:           AckFrame("r2", false, ReasonNotPending),
		`{"v":1,"t":"event","seq":0,"kind":"state_changed","payload":{"state":"running"}}`: EventFrame(0, "state_changed", "", map[string]any{"state": "running"}, ""),
	}
	for want, f := range cases {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Fatalf("shape drift:\nwant %s\ngot  %s", want, b)
		}
	}
}
