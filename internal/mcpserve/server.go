// Package mcpserve is the shared stdio MCP transport (orun-mcp UM0): one
// newline-delimited JSON-RPC 2.0 loop composing tool providers. The loop
// (16 MiB scanner, initialize/ping/tools handling, notification silence,
// error codes) moved verbatim from internal/workmcp; what is new is only
// the composition — `tools/list` merges provider rosters in order and
// `tools/call` routes to the owning provider. serverInfo is `orun` (the
// binary), never a per-provider name: the agent connects to one MCP and
// gets whatever tool planes the context supports. Since UM6 the loop also
// speaks resources/* and prompts/* — still hand-rolled (U-D1 stays open) —
// via the optional ResourceProvider/PromptProvider interfaces, advertised
// in capabilities only when a mounted provider supplies them.
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

// ToolDef is an MCP tool descriptor. Title is optional (the platform
// provider carries titles from the vendored manifest; work tools have
// none). Annotations are REQUIRED on every composed tool as of UM4 —
// checkRoster rejects a roster with incomplete annotations, so a strict
// client never has to guess a tool's read/write posture.
type ToolDef struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// AnnotationHints are the three wire-annotation hints every composed tool
// must carry (UM4): a strict client decides its prompting policy from
// these, and an absent hint reads as "unknown, treat as a write" — the
// field report's over-prompting failure.
var AnnotationHints = []string{"readOnlyHint", "destructiveHint", "idempotentHint"}

// Annotations builds a complete MCP tool-annotation set — the shape
// checkRoster requires on every ToolDef.
func Annotations(readOnly, destructive, idempotent bool) map[string]interface{} {
	return map[string]interface{}{
		"readOnlyHint":    readOnly,
		"destructiveHint": destructive,
		"idempotentHint":  idempotent,
	}
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

// ResourceTemplateDef is an MCP resource-template descriptor
// (resources/templates/list). Concrete resources are never enumerated:
// resources/list stays empty, matching the TS plane — agents discover ids
// via tools (catalog_search, runs_list) and attach the specific URI.
type ResourceTemplateDef struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent is one resources/read content block.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text"`
}

// ResourceProvider is the OPTIONAL interface a ToolProvider may additionally
// implement to serve resources (UM6) — type-asserted at dispatch, so
// tool-only providers need zero changes. ReadResource returns owned=false
// when the uri matches none of this provider's templates. An owned failure
// is a PROTOCOL-level error (resource reads have no isError channel — the TS
// plane's ResourceReadError posture) whose message carries the platform code.
type ResourceProvider interface {
	ResourceTemplates() []ResourceTemplateDef
	ReadResource(ctx context.Context, uri string) ([]ResourceContent, bool, error)
}

// PromptArg is one prompt argument declaration (prompts/list).
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// PromptDef is an MCP prompt descriptor.
type PromptDef struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description"`
	Arguments   []PromptArg `json:"arguments"`
}

// PromptProvider is the OPTIONAL interface a ToolProvider may additionally
// implement to serve prompts (UM6). RenderPrompt returns the prompt's single
// user-message text, owned=false when the name is not this provider's; the
// loop enforces required arguments from the advertised PromptDef, so render
// never validates.
type PromptProvider interface {
	Prompts() []PromptDef
	RenderPrompt(name string, args map[string]string) (string, bool)
}

// ForbiddenNameFragments must appear in no tool name on the merged roster:
// the WP-3/WP-10 work-plane invariants (no lifecycle write, no pin, no
// stored status) extend over every composed provider (README locked
// decision 5), and v4 (WH5) extends them with the human-only decisions —
// no approve, no adopt (V4-2: an agent cannot even NAME the decision).
// Tests sweep tools/list against these.
var ForbiddenNameFragments = []string{"status", "pin", "lifecycle", "approve", "adopt"}

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

// checkRoster rejects duplicate tool names across providers (a shadowed
// tool would be silently unreachable — names are disjoint by construction,
// saas-mcp-server locked decision 8; this is the belt to that suspender)
// and, since UM4, incomplete annotations: every composed tool must carry
// all three boolean hints, so wiring fails loudly at serve start instead
// of shipping a roster a strict client misreads as all-writes.
func (s *Server) checkRoster() error {
	seen := map[string]bool{}
	for _, p := range s.Providers {
		for _, t := range p.Tools() {
			if seen[t.Name] {
				return fmt.Errorf("mcpserve: duplicate tool name %q across providers — a provider would be silently shadowed", t.Name)
			}
			seen[t.Name] = true
			for _, hint := range AnnotationHints {
				if _, ok := t.Annotations[hint].(bool); !ok {
					return fmt.Errorf("mcpserve: tool %q is missing the %s annotation — every composed tool must advertise complete wire metadata (UM4)", t.Name, hint)
				}
			}
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

// resourceProviders returns the mounted providers that actually supply
// resource templates. Capabilities advertise `resources` only when this is
// non-empty (UM6) — a degraded or work-only serve keeps `{tools:{}}`.
func (s *Server) resourceProviders() []ResourceProvider {
	var out []ResourceProvider
	for _, p := range s.Providers {
		if rp, ok := p.(ResourceProvider); ok && len(rp.ResourceTemplates()) > 0 {
			out = append(out, rp)
		}
	}
	return out
}

// promptProviders is resourceProviders' prompt twin.
func (s *Server) promptProviders() []PromptProvider {
	var out []PromptProvider
	for _, p := range s.Providers {
		if pp, ok := p.(PromptProvider); ok && len(pp.Prompts()) > 0 {
			out = append(out, pp)
		}
	}
	return out
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
		// resources/prompts are advertised only when a mounted provider
		// supplies them (UM6): a degraded serve stays a tools-only server.
		capabilities := map[string]interface{}{"tools": map[string]interface{}{}}
		if len(s.resourceProviders()) > 0 {
			capabilities["resources"] = map[string]interface{}{}
		}
		if len(s.promptProviders()) > 0 {
			capabilities["prompts"] = map[string]interface{}{}
		}
		return ok(map[string]interface{}{
			"protocolVersion": ProtocolVersion,
			"capabilities":    capabilities,
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
	case "resources/list":
		if len(s.resourceProviders()) == 0 {
			return fail(-32601, "method not found: "+req.Method)
		}
		// Templates only — enumerating concrete resources would blow the
		// client's context budget for no navigational value (TS posture).
		return ok(map[string]interface{}{"resources": []interface{}{}})
	case "resources/templates/list":
		rps := s.resourceProviders()
		if len(rps) == 0 {
			return fail(-32601, "method not found: "+req.Method)
		}
		templates := []ResourceTemplateDef{}
		for _, rp := range rps {
			templates = append(templates, rp.ResourceTemplates()...)
		}
		return ok(map[string]interface{}{"resourceTemplates": templates})
	case "resources/read":
		rps := s.resourceProviders()
		if len(rps) == 0 {
			return fail(-32601, "method not found: "+req.Method)
		}
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.URI == "" {
			return fail(-32602, "invalid params: uri is required")
		}
		for _, rp := range rps {
			contents, owned, err := rp.ReadResource(ctx, params.URI)
			if !owned {
				continue
			}
			if err != nil {
				// No isError channel on reads: a protocol error carrying the
				// provider's coded message (`<code>: <detail>`).
				return fail(-32603, err.Error())
			}
			return ok(map[string]interface{}{"contents": contents})
		}
		return fail(-32002, "resource not found: "+params.URI+" matches no advertised template")
	case "prompts/list":
		pps := s.promptProviders()
		if len(pps) == 0 {
			return fail(-32601, "method not found: "+req.Method)
		}
		prompts := []PromptDef{}
		for _, pp := range pps {
			prompts = append(prompts, pp.Prompts()...)
		}
		return ok(map[string]interface{}{"prompts": prompts})
	case "prompts/get":
		pps := s.promptProviders()
		if len(pps) == 0 {
			return fail(-32601, "method not found: "+req.Method)
		}
		var params struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(-32602, "invalid params")
		}
		for _, pp := range pps {
			for _, def := range pp.Prompts() {
				if def.Name != params.Name {
					continue
				}
				for _, arg := range def.Arguments {
					if arg.Required && params.Arguments[arg.Name] == "" {
						return fail(-32602, fmt.Sprintf("prompt %s: missing required argument %q", def.Name, arg.Name))
					}
				}
				text, owned := pp.RenderPrompt(params.Name, params.Arguments)
				if !owned {
					break
				}
				return ok(map[string]interface{}{
					"description": def.Description,
					"messages": []map[string]interface{}{
						{"role": "user", "content": map[string]interface{}{"type": "text", "text": text}},
					},
				})
			}
		}
		return fail(-32602, "unknown prompt "+params.Name)
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}
