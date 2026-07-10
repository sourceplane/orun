package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// claudecode.go — the reference driver (specs/orun-agents-live/design.md §4):
// Claude Code headless, bidirectional stream-JSON. The harness process reads
// user turns and control responses on stdin and emits its event stream on
// stdout; this driver maps that wire onto the normalized Event vocabulary and
// serves the IO channels — steer to stdin, interrupt as a control request,
// approval requests bridged to the runtime's verdict loop.
//
// The wire protocol this driver implements is DOCUMENTED BY ITS FIXTURES
// (testdata/claude-*.ndjson + the fake-claude scenario harness): recorded
// captures pin every mapping row, so a harness upgrade that changes semantics
// is a failing test here, never a silent drift (risk R1). Live verification
// against a real binary runs behind ORUN_LIVE_DRIVER_SMOKE=1.

// ClaudeCodeID is the registered driver id (the `harness:` value agent types
// declare).
const ClaudeCodeID = "claude-code"

// ClaudeCode launches the Claude Code CLI headless. The zero value is the
// production configuration (binary "claude" on PATH).
type ClaudeCode struct {
	// Binary overrides the harness executable (default "claude"). Tests point
	// it at the fake-claude helper.
	Binary string
	// PrefixArgs are inserted before the computed arguments (the fake-claude
	// re-exec harness needs them; production leaves them empty).
	PrefixArgs []string
	// ExtraArgs are appended after the computed arguments (e.g. a pinned
	// --model, --allowedTools/--disallowedTools from the MCP setup).
	ExtraArgs []string
	// Env is appended to the process environment.
	Env []string
	// ResumeSession, when set, resumes an existing harness session
	// (--resume): the suspend/resume path hands back the harnessSession id a
	// prior run captured.
	ResumeSession string
}

func (d *ClaudeCode) ID() string { return ClaudeCodeID }

// Launch starts the harness and wires the stream. Non-blocking: events flow
// on io.Events until the process exits; Proc.Wait reports the terminal error.
func (d *ClaudeCode) Launch(ctx context.Context, b Brief, io IO) (Proc, error) {
	bin := d.Binary
	if bin == "" {
		bin = "claude"
	}
	args := append([]string(nil), d.PrefixArgs...)
	args = append(args,
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
	)
	if b.Instructions != "" {
		args = append(args, "--append-system-prompt", b.Instructions)
	}
	if io.MCPConfigPath != "" {
		args = append(args, "--mcp-config", io.MCPConfigPath)
	}
	if d.ResumeSession != "" {
		args = append(args, "--resume", d.ResumeSession)
	}
	args = append(args, d.ExtraArgs...)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = b.Workdir
	if len(d.Env) > 0 {
		cmd.Env = append(cmd.Environ(), d.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude-code: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude-code: stdout: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude-code: start: %w", err)
	}

	p := &claudeProc{cmd: cmd, done: make(chan struct{}), stderr: &stderr}
	w := &stdinWriter{w: stdin}

	// Reader: harness stdout → normalized events.
	go func() {
		defer close(p.done)
		d.readStream(ctx, stdout, io, w)
	}()
	// Pump: runtime inputs → harness stdin.
	go func() {
		defer stdin.Close()
		d.pump(ctx, b, io, w, p.done)
	}()
	return p, nil
}

// pump serializes every stdin write: the kickoff user turn, steers, verdicts
// (as control responses), and interrupts (as control requests).
func (d *ClaudeCode) pump(ctx context.Context, b Brief, io IO, w *stdinWriter, done <-chan struct{}) {
	kickoff := "Proceed with your brief now."
	if b.Task != "" {
		kickoff = "Proceed with your brief now. Your task is " + b.Task + "."
	}
	if d.ResumeSession != "" {
		kickoff = "The session was resumed. Continue where you left off."
	}
	w.writeUserText(kickoff)
	intSeq := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case m := <-io.Steer:
			w.writeUserText(m.Text)
		case v := <-io.Approve:
			w.writeControlResponse(v)
		case <-io.Interrupt:
			intSeq++
			w.writeControlRequest(fmt.Sprintf("orun-int-%d", intSeq), "interrupt")
		}
	}
}

// readStream maps harness stdout lines onto the normalized vocabulary. The
// mapping table lives in design.md §4.2; each row is pinned by a fixture.
func (d *ClaudeCode) readStream(ctx context.Context, r io.Reader, io IO, w *stdinWriter) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	turn := 0
	send := func(e Event) bool {
		select {
		case io.Events <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m claudeMsg
		if err := json.Unmarshal(line, &m); err != nil {
			if !send(Event{Kind: EventError, Text: "unparseable harness line: " + truncate(string(line), 200)}) {
				return
			}
			continue
		}
		switch m.Type {
		case "system":
			if m.Subtype == "init" {
				f := map[string]any{"phase": "init"}
				if m.SessionID != "" {
					f["harnessSession"] = m.SessionID
				}
				if m.Model != "" {
					f["model"] = m.Model
				}
				if !send(Event{Kind: EventHarness, Fields: f}) {
					return
				}
			}
		case "stream_event":
			if txt, ok := m.deltaText(); ok {
				if !send(Event{Kind: EventDelta, Text: txt, Fields: map[string]any{"turn": turn}}) {
					return
				}
			}
		case "assistant":
			turn++
			for _, blk := range m.contentBlocks() {
				switch blk.Type {
				case "text":
					if blk.Text != "" {
						if !send(Event{Kind: EventMessage, Text: blk.Text}) {
							return
						}
					}
				case "tool_use":
					if !send(Event{Kind: EventToolCall, Text: blk.Name,
						Fields: map[string]any{"tool": blk.Name, "toolUseId": blk.ID}}) {
						return
					}
				}
			}
		case "user":
			for _, blk := range m.contentBlocks() {
				if blk.Type == "tool_result" {
					if !send(Event{Kind: EventToolResult, Text: blk.resultText(),
						Fields: map[string]any{"toolUseId": blk.ToolUseID}}) {
						return
					}
				}
			}
		case "control_request":
			// The harness asks permission for a gated tool: bridge to the
			// runtime's approval loop; the verdict returns via io.Approve →
			// pump → control_response.
			if m.Request != nil && m.Request.Subtype == "can_use_tool" {
				if !send(Event{Kind: EventApproval, RequestID: m.RequestID, Text: "requesting " + m.Request.ToolName,
					Fields: map[string]any{"tool": m.Request.ToolName, "requestId": m.RequestID, "args": m.Request.Input}}) {
					return
				}
			} else {
				// Unknown control requests are answered success so the
				// harness never hangs on us (forward compatibility).
				w.writeControlAck(m.RequestID)
			}
		case "control_response":
			// Answers to our own control requests (interrupt): nothing to map.
		case "result":
			f := map[string]any{}
			if m.Usage != nil {
				f["tokens"] = m.Usage.InputTokens + m.Usage.OutputTokens
			}
			if m.TotalCostUSD > 0 {
				f["costUsd"] = m.TotalCostUSD
			}
			if m.DurationMS > 0 {
				f["durationMs"] = m.DurationMS
			}
			if m.NumTurns > 0 {
				f["numTurns"] = m.NumTurns
			}
			if !send(Event{Kind: EventCost, Fields: f}) {
				return
			}
			status := "completed"
			if m.Subtype != "success" {
				status = "failed"
			}
			done := map[string]any{"status": status}
			if m.SessionID != "" {
				done["harnessSession"] = m.SessionID
			}
			send(Event{Kind: EventDone, Text: m.Result, Fields: done})
		}
	}
}

// claudeProc is the running harness.
type claudeProc struct {
	cmd    *exec.Cmd
	done   chan struct{}
	stderr *strings.Builder
}

func (p *claudeProc) Wait() error {
	<-p.done // stdout fully drained first, so no event is lost
	err := p.cmd.Wait()
	if err != nil && p.stderr.Len() > 0 {
		return fmt.Errorf("%w: %s", err, truncate(p.stderr.String(), 500))
	}
	return err
}

// stdinWriter serializes harness stdin writes (kickoff, steers, control
// traffic race from two goroutines).
type stdinWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *stdinWriter) writeJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w.Write(append(b, '\n'))
}

func (s *stdinWriter) writeUserText(text string) {
	s.writeJSON(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	})
}

func (s *stdinWriter) writeControlResponse(v Verdict) {
	behavior := "deny"
	if v.Approved {
		behavior = "allow"
	}
	s.writeJSON(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": v.RequestID,
			"response":   map[string]any{"behavior": behavior, "message": v.Reason},
		},
	})
}

func (s *stdinWriter) writeControlAck(requestID string) {
	s.writeJSON(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   map[string]any{},
		},
	})
}

func (s *stdinWriter) writeControlRequest(requestID, subtype string) {
	s.writeJSON(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]any{"subtype": subtype},
	})
}

// claudeMsg is the harness stream-JSON envelope (only the fields the mapping
// consumes; everything else passes through untouched).
type claudeMsg struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	SessionID    string          `json:"session_id"`
	Model        string          `json:"model"`
	Message      *claudeMessage  `json:"message"`
	Event        *claudeSSEEvent `json:"event"`
	RequestID    string          `json:"request_id"`
	Request      *claudeControl  `json:"request"`
	Result       string          `json:"result"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	DurationMS   int             `json:"duration_ms"`
	NumTurns     int             `json:"num_turns"`
	Usage        *claudeUsage    `json:"usage"`
}

type claudeMessage struct {
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type claudeSSEEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type claudeControl struct {
	Subtype  string         `json:"subtype"`
	ToolName string         `json:"tool_name"`
	Input    map[string]any `json:"input"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (m *claudeMsg) contentBlocks() []claudeBlock {
	if m.Message == nil {
		return nil
	}
	return m.Message.Content
}

func (m *claudeMsg) deltaText() (string, bool) {
	if m.Event == nil || m.Event.Type != "content_block_delta" || m.Event.Delta == nil {
		return "", false
	}
	if m.Event.Delta.Type != "text_delta" || m.Event.Delta.Text == "" {
		return "", false
	}
	return m.Event.Delta.Text, true
}

// resultText renders a tool_result block's content (string or block list).
func (b claudeBlock) resultText() string {
	if len(b.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(b.Content, &blocks); err == nil {
		var parts []string
		for _, blk := range blocks {
			if blk.Text != "" {
				parts = append(parts, blk.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return truncate(string(b.Content), 400)
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil // swallow past the cap; stderr is diagnostics only
	}
	if len(p) > l.n {
		l.w.Write(p[:l.n])
		l.n = 0
		return len(p), nil
	}
	l.n -= len(p)
	return l.w.Write(p)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
