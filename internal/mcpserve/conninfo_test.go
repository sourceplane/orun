package mcpserve

import (
	"encoding/json"
	"strings"
	"testing"
)

func degradedInfo() ConnectionInfo {
	return ConnectionInfo{
		AuthState:  "absent",
		Work:       PlaneMount{Mounted: false, Reason: "auth did not resolve — not logged in to Orun Cloud; run `orun auth login`"},
		Platform:   PlaneMount{Mounted: false, Reason: "auth did not resolve — not logged in to Orun Cloud; run `orun auth login`"},
		Fix:        "run `orun auth login`",
		BackendURL: "",
	}
}

// TestConnectionInfoToolShape: exactly one tool, complete read-only
// annotations, and — deliberately — a name carrying NO forbidden fragment.
// The tool was planned as `auth_status`, which the WP-3/WP-10 sweep
// ("status" is a ForbiddenNameFragment) would reject; the sweep guards
// work-plane lifecycle vocabulary and was NOT weakened — the tool was
// renamed instead. This test pins that decision.
func TestConnectionInfoToolShape(t *testing.T) {
	p := &ConnectionInfoProvider{}
	tools := p.Tools()
	if len(tools) != 1 {
		t.Fatalf("built-in provider must serve exactly one tool, got %d", len(tools))
	}
	tool := tools[0]
	// The literal spelling is pinned here on purpose: renaming the constant
	// must not silently rename the wire surface.
	if tool.Name != "connection_info" {
		t.Fatalf("tool name = %q, want connection_info", tool.Name)
	}
	if ConnectionInfoToolName != "connection_info" {
		t.Fatalf("ConnectionInfoToolName = %q, want connection_info", ConnectionInfoToolName)
	}
	for _, frag := range ForbiddenNameFragments {
		if strings.Contains(tool.Name, frag) {
			t.Errorf("built-in tool name %q carries forbidden fragment %q — rename the tool, never weaken the sweep", tool.Name, frag)
		}
	}
	for _, hint := range AnnotationHints {
		if _, ok := tool.Annotations[hint].(bool); !ok {
			t.Errorf("missing %s annotation", hint)
		}
	}
	if ro, _ := tool.Annotations["readOnlyHint"].(bool); !ro {
		t.Error("connection_info must be read-only on the wire")
	}
	if d, _ := tool.Annotations["destructiveHint"].(bool); d {
		t.Error("connection_info must not be destructive")
	}
	if id, _ := tool.Annotations["idempotentHint"].(bool); !id {
		t.Error("connection_info must be idempotent")
	}
	// Not this provider's name → owned=false, so composition routing works.
	if _, owned := p.Call(t.Context(), "runs_list", nil); owned {
		t.Error("provider must not own foreign tool names")
	}
}

// TestDegradedServeHandshake (UM5's core promise): a server composed of
// ONLY the built-in provider — the no-credentials mount — answers
// initialize, lists exactly connection_info, and the call reports the
// degraded posture with the fix line and no secret material.
func TestDegradedServeHandshake(t *testing.T) {
	s := &Server{Providers: []ToolProvider{&ConnectionInfoProvider{Info: degradedInfo()}}, Version: "test"}
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		callLine(3, "connection_info", `{}`),
	)
	if _, ok := responses[0]["result"].(map[string]interface{})["serverInfo"]; !ok {
		t.Fatalf("degraded serve must answer initialize: %v", responses[0])
	}
	tools := responses[1]["result"].(map[string]interface{})["tools"].([]interface{})
	if len(tools) != 1 || tools[0].(map[string]interface{})["name"] != "connection_info" {
		t.Fatalf("degraded roster must be exactly [connection_info], got %v", tools)
	}

	text, isErr := resultText(t, responses[2])
	if isErr {
		t.Fatalf("connection_info itself must never be an error verdict: %q", text)
	}
	var out struct {
		Auth struct {
			State     string `json:"state"`
			Source    string `json:"source"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"auth"`
		BackendURL *string `json:"backendUrl"`
		Planes     map[string]struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"planes"`
		Fix    string `json:"fix"`
		Doctor string `json:"doctor"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("connection_info output is not JSON: %v\n%s", err, text)
	}
	if out.Auth.State != "absent" || out.Auth.Source != "" {
		t.Errorf("auth = %+v, want state absent with no source", out.Auth)
	}
	if out.BackendURL != nil {
		t.Errorf("backendUrl must be null when unresolved, got %v", *out.BackendURL)
	}
	for _, plane := range []string{"work", "platform"} {
		p, ok := out.Planes[plane]
		if !ok || p.State != "skipped" || !strings.Contains(p.Reason, "orun auth login") {
			t.Errorf("plane %s = %+v, want skipped with an actionable reason", plane, p)
		}
	}
	if out.Fix != "run `orun auth login`" {
		t.Errorf("fix = %q", out.Fix)
	}
	if !strings.Contains(out.Doctor, "orun mcp doctor") {
		t.Errorf("doctor pointer missing: %q", out.Doctor)
	}
}

// TestConnectionInfoAllOk: on a fully mounted serve the same tool is
// present and reports the healthy posture — no fix key at all.
func TestConnectionInfoAllOk(t *testing.T) {
	p := &ConnectionInfoProvider{Info: ConnectionInfo{
		AuthState:  "ok",
		AuthSource: "session",
		ExpiresAt:  "2026-07-12T15:04:05Z",
		BackendURL: "https://api.orun.cloud",
		Work:       PlaneMount{Mounted: true, Reason: "workspace ws_1 (from --workspace)"},
		Platform:   PlaneMount{Mounted: true, Reason: "auth resolved (session)"},
	}}
	result, owned := p.Call(t.Context(), ConnectionInfoToolName, nil)
	if !owned {
		t.Fatal("provider must own its tool")
	}
	text := result["content"].([]map[string]interface{})[0]["text"].(string)
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if _, hasFix := out["fix"]; hasFix {
		t.Errorf("healthy posture must carry no fix: %s", text)
	}
	auth := out["auth"].(map[string]interface{})
	if auth["state"] != "ok" || auth["source"] != "session" || auth["expiresAt"] != "2026-07-12T15:04:05Z" {
		t.Errorf("auth = %v", auth)
	}
	if out["backendUrl"] != "https://api.orun.cloud" {
		t.Errorf("backendUrl = %v", out["backendUrl"])
	}
	planes := out["planes"].(map[string]interface{})
	for _, plane := range []string{"work", "platform"} {
		if planes[plane].(map[string]interface{})["state"] != "mounted" {
			t.Errorf("plane %s = %v, want mounted", plane, planes[plane])
		}
	}
}
