package agentfold

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/agent/attach"
)

var update = flag.Bool("update", false, "rewrite golden files")

// foldFixture replays the head-bound frames of a shared attach fixture
// (internal/agent/attach/testdata — the same files the protocol and the
// cloud head are tested against) into a fresh conversation.
func foldFixture(t *testing.T, name string) *Conversation {
	t.Helper()
	path := filepath.Join("..", "..", "agent", "attach", "testdata", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("shared fixture %s: %v", path, err)
	}
	defer f.Close()

	conv := New()
	dec := attach.NewDecoder(f)
	for {
		frame, err := dec.Next()
		if err != nil {
			break
		}
		conv.Fold(frame)
	}
	return conv
}

// TestFoldParityGoldens pins the fold of every shared fixture. The console
// head folds the same streams; a semantic change here is a cross-head
// contract change and must be a reviewed diff (design §9, §12).
func TestFoldParityGoldens(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("..", "..", "agent", "attach", "testdata", "*.ndjson"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no shared fixtures found: %v", err)
	}
	for _, fx := range fixtures {
		name := filepath.Base(fx)
		t.Run(name, func(t *testing.T) {
			got := foldFixture(t, name).Transcript()
			goldenPath := filepath.Join("testdata", strings.TrimSuffix(name, ".ndjson")+".golden")
			if *update {
				_ = os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden (run -update): %v", err)
			}
			if string(want) != got {
				t.Errorf("fold drifted from golden.\n--- want\n%s\n--- got\n%s", want, got)
			}
		})
	}
}

// TestStreamingDeltaLifecycle: deltas accumulate as Streaming and are
// replaced — never duplicated — by the sealed message_agent event.
func TestStreamingDeltaLifecycle(t *testing.T) {
	c := New()
	c.Fold(attach.DeltaFrame(1, "let me "))
	c.Fold(attach.DeltaFrame(1, "check"))
	if c.Streaming != "let me check" {
		t.Fatalf("streaming = %q", c.Streaming)
	}
	c.Fold(attach.EventFrame(4, "message_agent", "", map[string]any{"text": "let me check the tests"}, ""))
	if c.Streaming != "" {
		t.Fatal("sealed turn must clear streaming buffer")
	}
	if len(c.Items) != 1 || c.Items[0].Text != "let me check the tests" {
		t.Fatalf("items = %+v", c.Items)
	}
}

// TestToolResultAttachesToCall: the card holds its own output.
func TestToolResultAttachesToCall(t *testing.T) {
	c := New()
	c.Fold(attach.EventFrame(1, "tool_call", "", map[string]any{"tool": "bash"}, ""))
	c.Fold(attach.EventFrame(2, "tool_result", "", map[string]any{"output": "3 tests passed\ndetail"}, ""))
	if len(c.Items) != 1 {
		t.Fatalf("result must fold into the call card: %+v", c.Items)
	}
	if !strings.HasPrefix(c.Items[0].Detail, "3 tests passed") {
		t.Fatalf("detail = %q", c.Items[0].Detail)
	}
}

// TestApprovalQueue: requests queue oldest-first and resolve by id.
func TestApprovalQueue(t *testing.T) {
	c := New()
	c.Fold(attach.EventFrame(1, "approval_requested", "", map[string]any{"requestId": "r1", "tool": "bash"}, ""))
	c.Fold(attach.EventFrame(2, "approval_requested", "", map[string]any{"requestId": "r2", "tool": "edit"}, ""))
	if len(c.Pending) != 2 || c.Pending[0].RequestID != "r1" {
		t.Fatalf("pending = %+v", c.Pending)
	}
	c.Fold(attach.EventFrame(3, "approval_resolved", "", map[string]any{"requestId": "r1", "approved": true, "principal": "usr_a"}, ""))
	if len(c.Pending) != 1 || c.Pending[0].RequestID != "r2" {
		t.Fatalf("pending after resolve = %+v", c.Pending)
	}
}

// TestTerminalStateEndsLive.
func TestTerminalStateEndsLive(t *testing.T) {
	c := New()
	c.Fold(attach.LiveFrame(0))
	if !c.Live {
		t.Fatal("live marker must set Live")
	}
	c.Fold(attach.EventFrame(9, "state_changed", "", map[string]any{"state": "completed"}, ""))
	if c.Live {
		t.Fatal("terminal state must clear Live")
	}
	if c.State != "completed" {
		t.Fatalf("state = %q", c.State)
	}
}

// TestRevAdvancesOnFold: the memo key material moves with state.
func TestRevAdvancesOnFold(t *testing.T) {
	c := New()
	r0 := c.Rev()
	c.Fold(attach.DeltaFrame(1, "x"))
	if c.Rev() == r0 {
		t.Fatal("fold must advance rev")
	}
	// Input-direction frames are ignored and must not advance rev.
	r1 := c.Rev()
	c.Fold(attach.SteerFrame("in-1", "hello"))
	if c.Rev() != r1 {
		t.Fatal("input frames must not fold")
	}
}
