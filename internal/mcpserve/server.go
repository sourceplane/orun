// Package mcpserve is the shared stdio MCP transport (orun-mcp UM0): one
// newline-delimited JSON-RPC 2.0 loop composing tool providers. The loop
// (16 MiB scanner, initialize/ping/tools handling, notification silence,
// error codes) moved verbatim from internal/workmcp; what is new is only
// the composition — `tools/list` merges provider rosters in order and
// `tools/call` routes to the owning provider. serverInfo is `orun` (the
// binary), never a per-provider name: the agent connects to one MCP and
// gets whatever tool planes the context supports.
package mcpserve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ProtocolVersion is the MCP protocol revision this loop speaks. The
// hand-rolled 2024-11-05 loop stays for now (design §2): it is what the
// work MCP already spoke and every mainstream client negotiates down.
const ProtocolVersion = "2024-11-05"

// ToolDef is an MCP tool descriptor.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Result is an MCP tools/call result: content blocks plus the optional
// isError flag.
type Result map[string]interface{}

// TextResult builds the standard single-text-block tool result; isErr marks
// a tool-level failure (MCP convention: a verdict the agent should reason
// about, not a protocol fault).
func TextResult(text string, isErr bool) Result {
	out := Result{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
	}
	if isErr {
		out["isError"] = true
	}
	return out
}

// ToolProvider is one tool plane mounted into the composed server. Call
// returns owned=false when the tool is not this provider's; owned providers
// map their own failures to isError Results — the transport never turns a
// tool failure into a protocol fault.
type ToolProvider interface {
	Tools() []ToolDef
	Call(ctx context.Context, name string, args json.RawMessage) (Result, bool)
}

// ForbiddenNameFragments must appear in no tool name on the merged roster:
// the WP-3/WP-10 work-plane invariants (no lifecycle write, no pin, no
// stored status) extend over every composed provider (README locked
// decision 5). Tests sweep tools/list against these.
var ForbiddenNameFragments = []string{"status", "pin", "lifecycle"}

// Server composes tool providers behind one stdio JSON-RPC loop.
type Server struct {
	Providers []ToolProvider
	Version   string // binary version, reported in serverInfo
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// checkRoster rejects duplicate tool names across providers: a shadowed
// tool would be silently unreachable, so wiring fails loudly at serve
// start instead. (Names are disjoint by construction — saas-mcp-server
// locked decision 8 — this is the belt to that suspender.)
func (s *Server) checkRoster() error {
	seen := map[string]bool{}
	for _, p := range s.Providers {
		for _, t := range p.Tools() {
			if seen[t.Name] {
				return fmt.Errorf("mcpserve: duplicate tool name %q across providers — a provider would be silently shadowed", t.Name)
			}
			seen[t.Name] = true
		}
	}
	return nil
}

// tools merges provider rosters in Providers order.
func (s *Server) tools() []ToolDef {
	all := []ToolDef{}
	for _, p := range s.Providers {
		all = append(all, p.Tools()...)
	}
	return all
}

// Serve reads newline-delimited JSON-RPC requests from r and writes
// responses to w until EOF. Notifications (no id) get no response.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	if err := s.checkRoster(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp := s.handle(ctx, &req)
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req *rpcRequest) *rpcResponse {
	if req.ID == nil {
		// notifications (e.g. notifications/initialized) get no response
		return nil
	}
	ok := func(result interface{}) *rpcResponse {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *rpcResponse {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}

	switch req.Method {
	case "initialize":
		return ok(map[string]interface{}{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "orun", "version": s.Version},
		})
	case "ping":
		return ok(map[string]interface{}{})
	case "tools/list":
		return ok(map[string]interface{}{"tools": s.tools()})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(-32602, "invalid params")
		}
		for _, p := range s.Providers {
			if result, owned := p.Call(ctx, params.Name, params.Arguments); owned {
				return ok(result)
			}
		}
		// No provider owns the name: the same isError result the work MCP
		// always returned — a verdict to reason about, not a protocol fault.
		return ok(TextResult("error: unknown tool "+params.Name, true))
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}
