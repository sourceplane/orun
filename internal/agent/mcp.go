package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// mcp.go — the driver's hands (specs/orun-agents/design.md §5, landed with
// orun-agents-live AL1): write the MCP config a driver points its harness at,
// filtered through the agent type's tool policy at write time. The orun MCP
// (`orun mcp serve`, internal/workmcp) is always present; the platform MCP is
// added when cloud-attached (AL4). Enforcement is layered: deny tools are
// absent from the reachable surface here, AND the runtime fold denies them
// again if a harness reports one anyway — the config is convenience, the
// runtime is the authority.

// MCPServer is one server entry in the driver's config.
type MCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`     // remote (Streamable HTTP) servers
	Type    string            `json:"type,omitempty"`    // "http" for remote servers
	Headers map[string]string `json:"headers,omitempty"` // e.g. the session bearer
}

// MCPSetup is the result of writing a driver MCP config: the file path plus
// the harness-level tool gates derived from the same policy. Allowed carries
// policy-allowed tools (pre-approved, no prompt); Disallowed carries denied
// tools (unreachable); ask-gated tools appear in neither list, so the harness
// prompts — and the prompt bridges to the runtime's approval loop.
type MCPSetup struct {
	ConfigPath string
	Allowed    []string
	Disallowed []string
}

// orunMCPToolName is the harness-visible name of an orun MCP tool.
func orunMCPToolName(tool string) string { return "mcp__orun__" + tool }

// WriteMCPConfig writes the driver MCP config into dir and derives the
// harness tool gates from policy over the orun MCP tool surface (toolNames —
// workmcp.ToolNames() in production; injected for tests). extra servers
// (the platform MCP, cloud-attached) are merged under their given names.
func WriteMCPConfig(dir string, policy ToolPolicy, toolNames []string, extra map[string]MCPServer) (MCPSetup, error) {
	servers := map[string]MCPServer{
		"orun": {Command: "orun", Args: []string{"mcp", "serve"}},
	}
	for name, srv := range extra {
		servers[name] = srv
	}
	cfg := map[string]any{"mcpServers": servers}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return MCPSetup{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MCPSetup{}, fmt.Errorf("agent: mcp config dir: %w", err)
	}
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return MCPSetup{}, fmt.Errorf("agent: mcp config: %w", err)
	}
	setup := MCPSetup{ConfigPath: path}
	for _, tool := range toolNames {
		switch policy.Decide(tool) {
		case DecisionAllow:
			setup.Allowed = append(setup.Allowed, orunMCPToolName(tool))
		case DecisionDeny:
			setup.Disallowed = append(setup.Disallowed, orunMCPToolName(tool))
		}
		// DecisionAsk: in neither list — the harness prompts, the prompt
		// becomes an approval_requested, a head answers.
	}
	sort.Strings(setup.Allowed)
	sort.Strings(setup.Disallowed)
	return setup, nil
}

// HarnessArgs renders the tool gates as Claude Code CLI arguments.
func (m MCPSetup) HarnessArgs() []string {
	var args []string
	if len(m.Allowed) > 0 {
		args = append(args, "--allowedTools", join(m.Allowed))
	}
	if len(m.Disallowed) > 0 {
		args = append(args, "--disallowedTools", join(m.Disallowed))
	}
	return args
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
