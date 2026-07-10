package attach

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/agent"
)

// socket.go — the local transport (attach-protocol.md §6.2): the body serves
// NDJSON frames on a same-uid unix socket; any process on the machine
// attaches as a head. Local trust model is the machine's (`.orun/` itself):
// the socket is 0600 in a 0700 directory and every local head is attributed
// to the CLI principal.

// LocalPrincipal is the attribution for same-uid local heads. Two terminals
// are the same human locally (risks Q6); the surface tag disambiguates.
const LocalPrincipal = "usr_cli"

// SocketListener serves one session's attach plane on a unix socket.
type SocketListener struct {
	ln   net.Listener
	path string
	wg   sync.WaitGroup
}

// ServeSocket binds path (0600, dir 0700) and serves srv's attach plane on
// it. Callers Close it when the session ends (Server.Close already ends every
// attached head; Close here just stops accepting).
func ServeSocket(srv *Server, path string) (*SocketListener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("attach: socket dir: %w", err)
	}
	_ = os.Remove(path) // a stale socket from a dead body is ours to replace
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("attach: listen: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("attach: socket mode: %w", err)
	}
	l := &SocketListener{ln: ln, path: path}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			l.wg.Add(1)
			go func() {
				defer l.wg.Done()
				serveConn(srv, conn)
			}()
		}
	}()
	return l, nil
}

// Close stops accepting and removes the socket file. Attached heads are ended
// by Server.Close, not here.
func (l *SocketListener) Close() error {
	err := l.ln.Close()
	_ = os.Remove(l.path)
	return err
}

// serveConn speaks the protocol on one accepted connection: the first frame
// must be attach{v, from, surface}; then frames pump both ways until either
// side closes.
func serveConn(srv *Server, conn net.Conn) {
	defer conn.Close()
	dec := NewDecoder(conn)
	first, err := dec.Next()
	if err != nil {
		return
	}
	if first.V != Version {
		WriteFrame(conn, ErrorFrame(CodeVersion, "unsupported protocol version"))
		return
	}
	if first.T != TAttach {
		WriteFrame(conn, ErrorFrame(CodeBadFrame, "first frame must be attach"))
		return
	}
	from := -1
	if first.From != nil {
		from = *first.From
	}
	head, err := srv.Attach(from, first.Surface, LocalPrincipal)
	if err != nil {
		WriteFrame(conn, ByeFrame(ReasonTerminal))
		return
	}
	// Writer: head feed → socket. Ends when the head closes (session over,
	// lagged, detach) or the write fails (peer gone). Closing the conn here
	// tells the peer the connection is over AND unblocks the reader below.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer conn.Close()
		for {
			f, ok := head.Recv()
			if !ok {
				return
			}
			if err := WriteFrame(conn, f); err != nil {
				head.Detach()
				return
			}
		}
	}()
	// Reader: socket → head inputs. EOF/error = the peer detached.
	for {
		f, err := dec.Next()
		if err != nil {
			head.Detach()
			break
		}
		head.Submit(f)
	}
	<-done
}

// ackTimeout bounds how long a socket client waits for an input's ack; a
// healthy body acks in microseconds, so a stuck wait means the body is gone.
const ackTimeout = 30 * time.Second

// ErrAckTimeout reports a socket input whose ack never arrived.
var ErrAckTimeout = errors.New("attach: timed out waiting for input ack")

// SocketClient is a head over the unix-socket transport. It mirrors the
// in-process HeadConn surface: Recv for the feed, sync-ack input methods.
type SocketClient struct {
	conn net.Conn

	mu      sync.Mutex
	refSeq  int
	pending map[string]chan Frame
	closed  bool

	frames chan Frame
}

// DialSocket attaches to a body's socket with a replay cursor. from = -1
// replays everything.
func DialSocket(path string, from int, surface string) (*SocketClient, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("attach: dial: %w", err)
	}
	c := &SocketClient{conn: conn, pending: map[string]chan Frame{}, frames: make(chan Frame, 256)}
	if err := WriteFrame(conn, AttachFrame(from, surface)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("attach: handshake: %w", err)
	}
	go c.readLoop()
	return c, nil
}

func (c *SocketClient) readLoop() {
	dec := NewDecoder(c.conn)
	for {
		f, err := dec.Next()
		if err != nil {
			break
		}
		switch f.T {
		case TAck:
			c.mu.Lock()
			ch := c.pending[f.Ref]
			delete(c.pending, f.Ref)
			c.mu.Unlock()
			if ch != nil {
				ch <- f
				continue
			}
			// Unmatched acks (another head's, over a relay) fall through to
			// the feed — they are harmless context.
			c.frames <- f
		case TPing:
			_ = c.send(PongFrame(f.At))
		default:
			c.frames <- f
		}
	}
	c.mu.Lock()
	c.closed = true
	for ref, ch := range c.pending {
		delete(c.pending, ref)
		close(ch)
	}
	c.mu.Unlock()
	close(c.frames)
}

// Frames is the body→head feed (hello, replay, live, events, deltas, bye).
// The channel closes when the connection ends.
func (c *SocketClient) Frames() <-chan Frame { return c.frames }

// Recv returns the next feed frame; ok=false when the connection is over.
func (c *SocketClient) Recv() (Frame, bool) {
	f, ok := <-c.frames
	return f, ok
}

func (c *SocketClient) send(f Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return agent.ErrSessionDone
	}
	return WriteFrame(c.conn, f)
}

// input sends a head input frame and blocks for its ack, mapping machine
// reasons back to the agent package's sentinel errors (the same contract the
// in-process HeadConn returns).
func (c *SocketClient) input(build func(ref string) Frame) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return agent.ErrSessionDone
	}
	c.refSeq++
	ref := fmt.Sprintf("in-%d", c.refSeq)
	ch := make(chan Frame, 1)
	c.pending[ref] = ch
	err := WriteFrame(c.conn, build(ref))
	c.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case ack, ok := <-ch:
		if !ok {
			return agent.ErrSessionDone
		}
		if ack.OK != nil && *ack.OK {
			return nil
		}
		switch ack.Reason {
		case ReasonNotPending:
			return agent.ErrNotPending
		default:
			return agent.ErrSessionDone
		}
	case <-time.After(ackTimeout):
		c.mu.Lock()
		delete(c.pending, ref)
		c.mu.Unlock()
		return ErrAckTimeout
	}
}

// Steer queues a user turn on the remote body.
func (c *SocketClient) Steer(text string) error {
	return c.input(func(ref string) Frame { return SteerFrame(ref, text) })
}

// Verdict answers a pending approval request.
func (c *SocketClient) Verdict(requestID string, approved bool, reason string) error {
	return c.input(func(ref string) Frame { return VerdictFrame(ref, requestID, approved, reason) })
}

// Interrupt stops the current turn.
func (c *SocketClient) Interrupt() error {
	return c.input(func(ref string) Frame { return InterruptFrame(ref) })
}

// End requests the graceful terminal.
func (c *SocketClient) End() error {
	return c.input(func(ref string) Frame { return EndFrame(ref) })
}

// Detach closes politely; the session continues.
func (c *SocketClient) Detach() {
	_ = c.send(DetachFrame())
	c.conn.Close()
}
