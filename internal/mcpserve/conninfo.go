package mcpserve

// The built-in connection reporter (orun-mcp UM5): the one tool mounted on
// EVERY serve — including a fully degraded one with no credentials, no
// backend URL, or a failed resolution — so an MCP client always completes
// initialize, always lists at least one tool, and can always ask the server
// itself why the other planes are (or are not) there. The provider only
// REPORTS a snapshot the command layer hands it at startup; it never calls
// the backend (deliberately no remotestate/workmcp/platformmcp imports) and
// never carries token material.

import (
	"context"
	"encoding/json"
)

// ConnectionInfoToolName is the built-in tool's wire name.
//
// Naming: this tool was planned as `auth_status`, but "status" is a
// ForbiddenNameFragment — the WP-3/WP-10 work-plane sweep bans lifecycle
// vocabulary (status/pin/lifecycle/approve/adopt) across the whole merged
// roster, and weakening the sweep to admit one built-in would weaken the
// work-plane invariant it guards. `connection_info` carries no forbidden
// fragment and still self-describes: it reports the connection's
// auth/backend/plane posture.
const ConnectionInfoToolName = "connection_info"

// PlaneMount is one tool plane's mount outcome: mounted or skipped, with
// the human-readable reason either way.
type PlaneMount struct {
	Mounted bool
	Reason  string
}

// ConnectionInfo is the startup snapshot the built-in tool reports. It is
// assembled by the command layer from the serve-time mount report; nothing
// here is secret material (states, sources, expiry timestamps, URLs — never
// tokens).
type ConnectionInfo struct {
	AuthState  string // ok | absent | expired | error
	AuthSource string // e.g. "GitHub Actions OIDC", "ORUN_TOKEN", "session"; empty when none resolved
	ExpiresAt  string // RFC3339 access-token expiry when known (session auth)
	BackendURL string // empty when unresolved (reported as null on the wire)
	Work       PlaneMount
	Platform   PlaneMount
	Fix        string // the exact command that repairs a degraded mount; empty when healthy
}

// ConnectionInfoProvider serves the always-present connection_info tool.
// It implements ToolProvider with exactly one read-only tool and holds a
// static snapshot — it reports, it does not probe.
type ConnectionInfoProvider struct {
	Info ConnectionInfo
}

// Tools implements ToolProvider: the single built-in tool, read-only
// (true/false/true — reading the snapshot mutates nothing and repeats
// identically).
func (p *ConnectionInfoProvider) Tools() []ToolDef {
	return []ToolDef{{
		Name:        ConnectionInfoToolName,
		Description: "Report this MCP server's connection posture: auth state and source (never token material), token expiry when known, backend URL, which tool planes mounted and why, and the exact fix when degraded. Always available, even with no credentials at all.",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Annotations: Annotations(true, false, true),
	}}
}

// Call implements ToolProvider: the snapshot rendered as one JSON object —
// {auth: {state, source?, expiresAt?}, backendUrl: string|null,
// planes: {work: {state, reason?}, platform: {state, reason?}},
// fix?: string, doctor: string}.
func (p *ConnectionInfoProvider) Call(_ context.Context, name string, _ json.RawMessage) (Result, bool) {
	if name != ConnectionInfoToolName {
		return nil, false
	}
	auth := map[string]interface{}{"state": p.Info.AuthState}
	if p.Info.AuthSource != "" {
		auth["source"] = p.Info.AuthSource
	}
	if p.Info.ExpiresAt != "" {
		auth["expiresAt"] = p.Info.ExpiresAt
	}
	var backendURL interface{} // null when unresolved
	if p.Info.BackendURL != "" {
		backendURL = p.Info.BackendURL
	}
	payload := map[string]interface{}{
		"auth":       auth,
		"backendUrl": backendURL,
		"planes": map[string]interface{}{
			"work":     planePayload(p.Info.Work),
			"platform": planePayload(p.Info.Platform),
		},
		"doctor": "run `orun mcp doctor` for a full check",
	}
	if p.Info.Fix != "" {
		payload["fix"] = p.Info.Fix
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return TextResult("error: "+err.Error(), true), true
	}
	return TextResult(string(b), false), true
}

// planePayload renders one plane's mount outcome for the wire.
func planePayload(m PlaneMount) map[string]interface{} {
	state := "skipped"
	if m.Mounted {
		state = "mounted"
	}
	out := map[string]interface{}{"state": state}
	if m.Reason != "" {
		out["reason"] = m.Reason
	}
	return out
}
