package driver

import (
	"bufio"
	"bytes"
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

// launchArgs assembles the harness argument list (factored for the args
// contract test — the flag set IS driver behavior, same discipline as the
// stream fixtures).
func (d *ClaudeCode) launchArgs(b Brief, io IO) []string {
	args := append([]string(nil), d.PrefixArgs...)
	args = append(args,
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		// Route permission checks over the stream: with this flag the harness
		// emits control_request{subtype:"can_use_tool"} for every gated tool
		// and BLOCKS the call until our control_response verdict arrives — the
		// exact wire the readStream bridge and the runtime approval loop were
		// built for (and the fixtures record). Without it the harness resolved
		// permissions from the sandbox's own claude config, so ask-lane tools
		// ran ungated and no approval card ever reached a head.
		"--permission-prompt-tool", "stdio",
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
	return append(args, d.ExtraArgs...)
}

// Launch starts the harness and wires the stream. Non-blocking: events flow
// on io.Events until the process exits; Proc.Wait reports the terminal error.
func (d *ClaudeCode) Launch(ctx context.Context, b Brief, io IO) (Proc, error) {
	bin := d.Binary
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, d.launchArgs(b, io)...)
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
					fields := map[string]any{"tool": blk.Name, "toolUseId": blk.ID}
					// Carry the input so the durable event can say WHAT ran —
					// a tool row reading only its policy decision told the
					// operator nothing (observed live in the cockpit).
					if args := compactArgs(blk.Input); args != "" {
						fields["args"] = args
					}
					if !send(Event{Kind: EventToolCall, Text: blk.Name, Fields: fields}) {
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
				// Keep the request's input: the allow verdict must echo it
				// back as `updatedInput` (required by the harness's schema).
				w.rememberInput(m.RequestID, m.Request.Input)
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
				// `tokens` keeps its meaning (metering reads it); the split
				// beside it makes the prompt cache visible.
				f["tokens"] = m.Usage.InputTokens + m.Usage.OutputTokens
				f["inputTokens"] = m.Usage.InputTokens
				f["outputTokens"] = m.Usage.OutputTokens
				f["cacheReadTokens"] = m.Usage.CacheReadInputTokens
				f["cacheWriteTokens"] = m.Usage.CacheCreationInputTokens
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
	// pendingInputs remembers each can_use_tool request's input by request id:
	// the harness's allow branch REQUIRES `updatedInput` (a record), and the
	// only honest record to return is the input it asked about, unchanged.
	pendingInputs map[string]map[string]any
}

// rememberInput stores a control request's input for the verdict write.
func (s *stdinWriter) rememberInput(requestID string, input map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingInputs == nil {
		s.pendingInputs = map[string]map[string]any{}
	}
	s.pendingInputs[requestID] = input
}

// takeInput returns (and forgets) the stored input for a request id.
func (s *stdinWriter) takeInput(requestID string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.pendingInputs[requestID]
	delete(s.pendingInputs, requestID)
	return in
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
	// The can_use_tool result contract (the harness validates it strictly):
	//   {behavior: "allow", updatedInput: record}  |  {behavior: "deny", message: string}
	// `updatedInput` became REQUIRED on the allow branch (observed live: a
	// bare allow failed the harness's union with "expected record, received
	// undefined" and every approved call errored) — we return the request's
	// own input, unchanged. An allow carries NO message field; a deny must
	// always carry a non-empty message — it becomes the tool error the model
	// reads, so an empty reason would deny without saying why.
	var result map[string]any
	if v.Approved {
		input := s.takeInput(v.RequestID)
		if input == nil {
			input = map[string]any{}
		}
		result = map[string]any{"behavior": "allow", "updatedInput": input}
	} else {
		s.takeInput(v.RequestID) // forget the stored input either way
		msg := v.Reason
		if msg == "" {
			msg = "denied"
		}
		result = map[string]any{"behavior": "deny", "message": msg}
	}
	s.writeJSON(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": v.RequestID,
			"response":   result,
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
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

// argsCap bounds the tool-input string that rides a tool_call event: the
// cockpit needs to show WHAT ran, not archive it — a durable event is not a
// place for a 200 KB Write payload.
const argsCap = 2000

// compactArgs renders a tool_use input as a single compact JSON string,
// capped. Empty or unparseable input reads as "" — the event simply carries
// no args rather than garbage.
func compactArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	s := buf.String()
	if s == "{}" || s == "null" {
		return ""
	}
	if len(s) > argsCap {
		s = s[:argsCap] + "…"
	}
	return s
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
	// The prompt cache (saas-session-continuity CT3): what was read from it
	// and written to it. input_tokens is only the uncached remainder.
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
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
