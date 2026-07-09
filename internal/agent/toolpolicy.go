package agent

import (
	"path"

	"github.com/sourceplane/orun/internal/nodes"
)

// Decision is the runtime's verdict on a tool call, enforced between the driver
// and the MCP (specs/orun-agents/design.md §5). Deny-by-default.
type Decision int

const (
	// DecisionDeny — the tool is unreachable (absent from the driver's config).
	DecisionDeny Decision = iota
	// DecisionAllow — passes through.
	DecisionAllow
	// DecisionAsk — intercept, emit approval_requested, block for a verdict.
	DecisionAsk
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionAsk:
		return "ask"
	default:
		return "deny"
	}
}

// ToolPolicy evaluates a tool name against an agent type's allow/ask/deny
// globs. Precedence is deny > ask > allow, each matched most-specifically: an
// explicit non-wildcard match beats a wildcard one, so `deny: ["*"]` +
// `allow: ["work_get"]` allows exactly work_get. When nothing matches, the
// result is Deny (deny-by-default).
type ToolPolicy struct {
	allow, ask, deny []string
}

// NewToolPolicy builds a policy from an agent type's tool block.
func NewToolPolicy(p nodes.AgentToolPolicy) ToolPolicy {
	return ToolPolicy{allow: p.Allow, ask: p.Ask, deny: p.Deny}
}

// Decide returns the decision for a tool name.
func (t ToolPolicy) Decide(tool string) Decision {
	dExact, dWild := match(t.deny, tool)
	kExact, kWild := match(t.ask, tool)
	aExact, aWild := match(t.allow, tool)

	// An exact (non-wildcard) listing wins over any wildcard, across lists,
	// in deny > ask > allow order.
	switch {
	case dExact:
		return DecisionDeny
	case kExact:
		return DecisionAsk
	case aExact:
		return DecisionAllow
	}
	// No exact listing: fall back to wildcard matches, same precedence.
	switch {
	case aWild:
		return DecisionAllow
	case kWild:
		return DecisionAsk
	case dWild:
		return DecisionDeny
	default:
		return DecisionDeny
	}
}

// match reports whether tool matches any pattern, distinguishing an exact
// (literal, no glob metacharacters) match from a wildcard one.
func match(patterns []string, tool string) (exact, wild bool) {
	for _, p := range patterns {
		if p == tool {
			exact = true
			continue
		}
		if isGlob(p) {
			if ok, err := path.Match(p, tool); err == nil && ok {
				wild = true
			}
		}
	}
	return exact, wild
}

func isGlob(p string) bool {
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '*', '?', '[':
			return true
		}
	}
	return false
}
