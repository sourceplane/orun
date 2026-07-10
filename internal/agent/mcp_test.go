package agent

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
)

func TestWriteMCPConfigFiltersThroughPolicy(t *testing.T) {
	dir := t.TempDir()
	policy := NewToolPolicy(nodes.AgentToolPolicy{
		Allow: []string{"work_query", "work_get"},
		Ask:   []string{"contract_propose"},
		Deny:  []string{"*"},
	})
	tools := []string{"work_query", "work_get", "task_assign", "contract_propose"}
	setup, err := WriteMCPConfig(dir, policy, tools, nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(setup.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	orun, ok := cfg.MCPServers["orun"]
	if !ok || orun.Command != "orun" || len(orun.Args) != 2 {
		t.Fatalf("orun MCP server entry = %+v", orun)
	}

	wantAllow := []string{"mcp__orun__work_get", "mcp__orun__work_query"}
	if !reflect.DeepEqual(setup.Allowed, wantAllow) {
		t.Fatalf("allowed = %v", setup.Allowed)
	}
	// Denied tools are gated at the harness too; ask-gated tools are in
	// NEITHER list — the harness prompts and the prompt becomes an
	// approval_requested.
	wantDeny := []string{"mcp__orun__task_assign"}
	if !reflect.DeepEqual(setup.Disallowed, wantDeny) {
		t.Fatalf("disallowed = %v", setup.Disallowed)
	}

	args := setup.HarnessArgs()
	want := []string{"--allowedTools", "mcp__orun__work_get,mcp__orun__work_query",
		"--disallowedTools", "mcp__orun__task_assign"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("harness args = %v", args)
	}

	// The file is private: it may later carry remote-server headers.
	info, err := os.Stat(setup.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteMCPConfigMergesExtraServers(t *testing.T) {
	dir := t.TempDir()
	setup, err := WriteMCPConfig(dir, ToolPolicy{}, nil, map[string]MCPServer{
		"platform": {URL: "https://mcp.example.com/v1", Type: "http",
			Headers: map[string]string{"Authorization": "Bearer tok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(setup.ConfigPath)
	var cfg struct {
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["platform"].URL != "https://mcp.example.com/v1" {
		t.Fatalf("platform server missing: %+v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["orun"]; !ok {
		t.Fatal("orun server must always be present")
	}
}
