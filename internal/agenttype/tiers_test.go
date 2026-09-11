package agenttype

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/penmcp"
	"github.com/sourceplane/orun/internal/platformmcp"
)

// TestShippedTierMatrices: the shipped agent types carry the allow/ask/deny
// trust tiers, and the tiers keep their promises.
//
// The matrix this test was written for (orun-initiatives-v2 IS4) guarded a
// 45-tool work-plane roster: no signature tool in an allow lane, and every
// work-shaped name a policy speaks must exist on that roster, because a
// typo'd entry is a silent deny-by-omission. The work plane is gone
// (orun-work-teardown WT2) and with it every signature; what survives is
// the second promise, which is the one that caught a real shipped gap —
// a lane naming a tool the local MCP does not serve grants nothing, and
// nothing tells you.
func TestShippedTierMatrices(t *testing.T) {
	t.Chdir(t.TempDir()) // force the embedded copies — test what ships

	// The names the local orun MCP serves natively. Platform-plane and
	// harness tools are checked by shape below, not by roster: the
	// platform roster is pinned to the vendored manifest in
	// internal/platformmcp, and harness tools are the driver's.
	local := map[string]bool{}
	for _, name := range penmcp.ToolNames() {
		local[name] = true
	}
	// The platform plane is served by this binary too (composed with the pen
	// in `orun mcp serve`), so its vendored roster is part of what a policy
	// may name — epic_create / milestone_create / task_get / task_list are
	// the bootstrapper's programme tools.
	for _, name := range platformmcp.ToolNames() {
		local[name] = true
	}
	local["connection_info"] = true // mcpserve's built-in, mounted on every serve

	// A lowercase snake_case name is an MCP tool name; harness tools are
	// capitalized (Read, Bash, …). catalog_* is served by the catalog MCP
	// the cockpit attaches, not by this binary.
	isLocalMCPName := func(name string) bool {
		if name == "" || strings.ToLower(name[:1]) != name[:1] {
			return false
		}
		return !strings.HasPrefix(name, "catalog_")
	}

	for _, typeName := range []string{"implementer", "orchestrator", "bootstrapper"} {
		d, issues := LoadNamed(typeName)
		if d == nil {
			t.Fatalf("%s: %v", typeName, issues)
		}
		for lane, names := range map[string][]string{"allow": d.Tools.Allow, "ask": d.Tools.Ask} {
			for _, name := range names {
				if isLocalMCPName(name) && !local[name] {
					t.Errorf("%s: %s lane names %q — not on the local MCP roster (typo = silent deny)", typeName, lane, name)
				}
			}
		}
		if len(d.Tools.Deny) == 0 || d.Tools.Deny[len(d.Tools.Deny)-1] != "*" {
			t.Errorf("%s: deny lane must backstop with %q (deny-by-default), got %v", typeName, "*", d.Tools.Deny)
		}
	}

	// The pen is the working seat's one write onto the world outside the
	// checkout, and it is exactly the gesture the cloud's orun/compliance
	// check verifies. An implementer and an unattended bootstrap both owe
	// it; a planner does not write PRs.
	for _, typeName := range []string{"implementer", "bootstrapper"} {
		d, _ := LoadNamed(typeName)
		allow := map[string]bool{}
		for _, n := range d.Tools.Allow {
			allow[n] = true
		}
		if !allow[penmcp.ToolName] {
			t.Errorf("%s: %s must be allow — one task, one PR, lineage in the body", typeName, penmcp.ToolName)
		}
	}
}

// TestRetiredTypesAreGone: epic-owner was the work plane's seat — its whole
// loop was designs, adoption, approval and the milestone ladder. It went
// with the plane (WT2). A shipped type that names a roster nobody serves is
// worse than no type at all: every call auto-denies and the session looks
// broken rather than unsupported.
func TestRetiredTypesAreGone(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, retired := range []string{"epic-owner", "initiative-owner"} {
		if d, _ := LoadNamed(retired); d != nil {
			t.Errorf("agent type %q still ships — it was retired with the work plane", retired)
		}
	}
}
