package attach

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/agent"
)

// sockPath returns a short socket path (unix sockets cap around ~104 bytes;
// t.TempDir can be deep on some CI).
func sockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "s.sock")
}

// clientReader mirrors headReader for SocketClient feeds.
type clientReader struct{ ch <-chan Frame }

func (r clientReader) waitFor(t *testing.T, what string, pred func(Frame) bool) Frame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-r.ch:
			if !ok {
				t.Fatalf("waiting for %s: feed closed", what)
			}
			if pred(f) {
				return f
			}
		case <-deadline:
			t.Fatalf("waiting for %s: timed out", what)
		}
	}
}

func TestSocketLifecycleTwoTerminals(t *testing.T) {
	srv, _, resCh, _ := startInteractive(t, "as_sock1")
	path := sockPath(t)
	ln, err := ServeSocket(srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Terminal one attaches over the socket.
	c1, err := DialSocket(path, -1, "cli")
	if err != nil {
		t.Fatal(err)
	}
	r1 := clientReader{ch: c1.Frames()}
	hello := r1.waitFor(t, "hello", func(f Frame) bool { return f.T == THello })
	if hello.SessionID != "as_sock1" {
		t.Fatalf("hello = %+v", hello)
	}
	r1.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })

	// Steer from terminal one: sync ack, attributed to the local principal.
	if err := c1.Steer("hello over the wire"); err != nil {
		t.Fatal(err)
	}
	mu := r1.waitFor(t, "message_user", func(f Frame) bool { return f.T == TEvent && f.Kind == "message_user" })
	if mu.Payload["principal"] != LocalPrincipal {
		t.Fatalf("principal = %v", mu.Payload["principal"])
	}

	// Terminal two joins mid-run, replays, and answers the approval that
	// terminal one requested.
	if err := c1.Steer("/ask contract_propose"); err != nil {
		t.Fatal(err)
	}
	ask := r1.waitFor(t, "ask", func(f Frame) bool { return f.T == TEvent && f.Kind == "approval_requested" })
	reqID, _ := ask.Payload["requestId"].(string)

	c2, err := DialSocket(path, -1, "cli")
	if err != nil {
		t.Fatal(err)
	}
	r2 := clientReader{ch: c2.Frames()}
	r2.waitFor(t, "replayed ask", func(f Frame) bool { return f.T == TEvent && f.Kind == "approval_requested" })
	if err := c2.Verdict(reqID, true, "lgtm"); err != nil {
		t.Fatal(err)
	}
	r1.waitFor(t, "resolution", func(f Frame) bool { return f.T == TEvent && f.Kind == "approval_resolved" })

	// Late verdict from terminal one: the machine reason maps back to the
	// sentinel error through the ack.
	if err := c1.Verdict(reqID, false, "late"); !errors.Is(err, agent.ErrNotPending) {
		t.Fatalf("late verdict = %v", err)
	}

	// Detach terminal two (session survives), end from terminal one.
	c2.Detach()
	if err := c1.Steer("/done"); err != nil {
		t.Fatal(err)
	}
	res := <-resCh
	if res.Outcome.Status != "completed" {
		t.Fatalf("outcome = %+v", res.Outcome)
	}
	r1.waitFor(t, "bye", func(f Frame) bool { return f.T == TBye })
}

func TestSocketRejectsBadHandshake(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_sock2"}, nil)
	path := sockPath(t)
	ln, err := ServeSocket(srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Wrong version.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	WriteFrame(conn, Frame{V: 99, T: TAttach})
	f, err := NewDecoder(conn).Next()
	if err != nil || f.T != TError || f.Code != CodeVersion {
		t.Fatalf("got %+v, %v", f, err)
	}
	conn.Close()

	// Wrong first frame.
	conn2, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	WriteFrame(conn2, SteerFrame("r1", "hi"))
	f2, err := NewDecoder(conn2).Next()
	if err != nil || f2.T != TError || f2.Code != CodeBadFrame {
		t.Fatalf("got %+v, %v", f2, err)
	}
	conn2.Close()
}

func TestSocketClosedServerSaysBye(t *testing.T) {
	srv := NewServer(SessionInfo{SessionID: "as_sock3"}, nil)
	path := sockPath(t)
	ln, err := ServeSocket(srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv.Close("terminal")
	c, err := DialSocket(path, -1, "cli")
	if err != nil {
		t.Fatal(err)
	}
	r := clientReader{ch: c.Frames()}
	r.waitFor(t, "bye", func(f Frame) bool { return f.T == TBye })
}

func TestSocketClientInputAfterCloseIsSessionDone(t *testing.T) {
	srv, _, resCh, _ := startInteractive(t, "as_sock4")
	path := sockPath(t)
	ln, err := ServeSocket(srv, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := DialSocket(path, -1, "cli")
	if err != nil {
		t.Fatal(err)
	}
	r := clientReader{ch: c.Frames()}
	r.waitFor(t, "live", func(f Frame) bool { return f.T == TLive })
	if err := c.End(); err != nil {
		t.Fatal(err)
	}
	<-resCh
	r.waitFor(t, "bye", func(f Frame) bool { return f.T == TBye })
	// Wait for the read loop to observe the close, then submit.
	deadline := time.After(5 * time.Second)
	for {
		if err := c.Steer("too late"); errors.Is(err, agent.ErrSessionDone) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("steer after close never returned ErrSessionDone")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
