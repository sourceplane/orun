package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
)

// serveRPC drives a composed server over one in-memory stdio exchange and
// decodes the newline-delimited responses.
func serveRPC(t *testing.T, s *mcpserve.Server, lines ...string) []map[string]interface{} {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		responses = append(responses, m)
	}
	return responses
}

func degradedReport() mcpMountReport {
	rep := mcpMountReport{authState: "absent", authDetail: errNotLoggedIn().Error(), backendURL: "https://bogus.example"}
	return rep.planesSkipped("auth did not resolve — " + rep.authDetail)
}

// TestDegradedServeAssembly (UM5): with no auth at all the assembled server
// still answers initialize and lists EXACTLY connection_info — never an
// exit before the handshake.
func TestDegradedServeAssembly(t *testing.T) {
	providers := assembleMcpProviders(nil, degradedReport(), false)
	if len(providers) != 1 {
		t.Fatalf("degraded assembly = %d providers, want only the built-in", len(providers))
	}
	s := &mcpserve.Server{Providers: providers, Version: "test"}
	responses := serveRPC(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"connection_info","arguments":{}}}`,
	)
	info := responses[0]["result"].(map[string]interface{})["serverInfo"].(map[string]interface{})
	if info["name"] != "orun" {
		t.Fatalf("degraded initialize serverInfo = %v", info)
	}
	// UM6: a degraded serve stays a tools-only server — no provider supplies
	// resources/prompts, so capabilities must not advertise them.
	caps := responses[0]["result"].(map[string]interface{})["capabilities"].(map[string]interface{})
	if _, ok := caps["resources"]; ok {
		t.Fatalf("degraded serve must not advertise resources: %v", caps)
	}
	if _, ok := caps["prompts"]; ok {
		t.Fatalf("degraded serve must not advertise prompts: %v", caps)
	}
	tools := responses[1]["result"].(map[string]interface{})["tools"].([]interface{})
	if len(tools) != 1 || tools[0].(map[string]interface{})["name"] != "connection_info" {
		t.Fatalf("degraded roster = %v, want exactly [connection_info]", tools)
	}
	result := responses[2]["result"].(map[string]interface{})
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("connection_info must answer, not error: %q", text)
	}
	if !strings.Contains(text, `"state":"absent"`) || !strings.Contains(text, "run `orun auth login`") {
		t.Fatalf("degraded connection_info lacks state/fix: %q", text)
	}
}

// TestFullyMountedAssemblyRoster: with auth + workspace the roster is the
// full merged surface PLUS connection_info (counts derived from the live
// rosters — UM4 — with the field report's 40+1 pinned literally so a silent
// shrink fails loudly), and connection_info reports the all-ok posture.
func TestFullyMountedAssemblyRoster(t *testing.T) {
	// A real client, never called: Tools() is roster-only.
	client := remotestate.NewClientWithScope("http://127.0.0.1:1", "test", remotestate.NewStaticTokenSource("dummy"), remotestate.Scope{OrgID: "ws_1"})
	rep := mcpMountReport{
		backendURL:      "http://127.0.0.1:1",
		authState:       "ok",
		authSource:      "ORUN_TOKEN",
		workspace:       "ws_1",
		workspaceSource: "--workspace",
		workMounted:     true, workReason: "workspace ws_1 (from --workspace)",
		platformMounted: true, platformReason: "auth resolved (ORUN_TOKEN)",
	}
	providers := assembleMcpProviders(client, rep, false)
	s := &mcpserve.Server{Providers: providers, Version: "test"}
	responses := serveRPC(t, s,
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"connection_info","arguments":{}}}`,
	)
	// UM6: the platform plane supplies resources + prompts, so a fully
	// mounted serve advertises all three capabilities.
	caps := responses[0]["result"].(map[string]interface{})["capabilities"].(map[string]interface{})
	for _, key := range []string{"tools", "resources", "prompts"} {
		if _, ok := caps[key]; !ok {
			t.Fatalf("fully mounted capabilities lack %s: %v", key, caps)
		}
	}
	responses = responses[1:]
	tools := responses[0]["result"].(map[string]interface{})["tools"].([]interface{})
	counts := countMcpRoster()
	if want := counts.work + counts.platform + counts.server; len(tools) != want {
		t.Fatalf("fully mounted roster = %d tools, want %d", len(tools), want)
	}
	if len(tools) != 41 {
		t.Fatalf("fully mounted roster = %d tools, want 41 (40 + connection_info)", len(tools))
	}
	last := tools[len(tools)-1].(map[string]interface{})
	if last["name"] != "connection_info" {
		t.Fatalf("connection_info must ride every serve; last tool = %v", last["name"])
	}
	text := responses[1]["result"].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, `"state":"ok"`) || strings.Contains(text, `"fix"`) {
		t.Fatalf("all-ok connection_info wrong: %q", text)
	}
}

// TestConnectionInfoFixSelection: the fix line matches the miss — missing
// backend URL points at ORUN_BACKEND_URL, any non-ok auth at login, and a
// healthy mount carries none.
func TestConnectionInfoFixSelection(t *testing.T) {
	noBackend := mcpMountReport{authState: "absent", backendErr: "missing backend URL"}
	if fix := noBackend.connectionInfo().Fix; fix != "set ORUN_BACKEND_URL or run `orun auth login`" {
		t.Errorf("no-backend fix = %q", fix)
	}
	absent := mcpMountReport{backendURL: "https://api.example", authState: "absent"}
	if fix := absent.connectionInfo().Fix; fix != "run `orun auth login`" {
		t.Errorf("absent fix = %q", fix)
	}
	expired := mcpMountReport{backendURL: "https://api.example", authState: "expired", authSource: "session"}
	if fix := expired.connectionInfo().Fix; fix != "run `orun auth login`" {
		t.Errorf("expired fix = %q", fix)
	}
	ok := mcpMountReport{backendURL: "https://api.example", authState: "ok", authSource: "session"}
	if fix := ok.connectionInfo().Fix; fix != "" {
		t.Errorf("healthy mount must carry no fix, got %q", fix)
	}
}

// TestSessionMountState: the structured session classification behind both
// serve's mount report and doctor's line — expired-with-live-refresh is ok,
// fully expired is "expired", and nothing ever contains token material.
func TestSessionMountState(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	secret, refSecret := "tok_SECRET_ACCESS", "tok_SECRET_REFRESH"

	if state, detail, _ := sessionMountState(nil, now); state != "absent" || !strings.Contains(detail, "orun auth login") {
		t.Errorf("nil session: (%q, %q)", state, detail)
	}

	live := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(10 * time.Minute).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(24 * time.Hour).Format(time.RFC3339),
		User:               cliauth.SessionUser{Email: "dev@example.com"},
	}
	state, detail, expiresAt := sessionMountState(live, now)
	if state != "ok" || !strings.Contains(detail, "dev@example.com") || expiresAt != now.Add(10*time.Minute).Format(time.RFC3339) {
		t.Errorf("live session: (%q, %q, %q)", state, detail, expiresAt)
	}

	refreshable := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	if state, detail, _ := sessionMountState(refreshable, now); state != "ok" || !strings.Contains(detail, "refresh token live") {
		t.Errorf("refreshable session: (%q, %q)", state, detail)
	}

	expired := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(-time.Hour).Format(time.RFC3339),
	}
	state, detail, expiresAt = sessionMountState(expired, now)
	if state != "expired" || !strings.Contains(detail, "orun auth login") || expiresAt != "" {
		t.Errorf("fully expired session: (%q, %q, %q)", state, detail, expiresAt)
	}
	for _, s := range []string{detail, expiresAt} {
		if strings.Contains(s, secret) || strings.Contains(s, refSecret) {
			t.Fatalf("session state leaks token material: %q", s)
		}
	}
}

// TestMcpServeLine: the default single stderr line names the degradation
// and its fix; the healthy shapes stay exactly as before UM5.
func TestMcpServeLine(t *testing.T) {
	full := mcpMountReport{backendURL: "https://api.example", workspace: "ws_1", workMounted: true, platformMounted: true}
	if line := mcpServeLine(full); !strings.Contains(line, "workspace ws_1; work + platform tools") {
		t.Errorf("full line = %q", line)
	}
	platformOnly := mcpMountReport{backendURL: "https://api.example", platformMounted: true}
	if line := mcpServeLine(platformOnly); !strings.Contains(line, "platform tools only") {
		t.Errorf("platform-only line = %q", line)
	}
	if line := mcpServeLine(degradedReport()); !strings.Contains(line, "degraded: not logged in") || !strings.Contains(line, "orun auth login") {
		t.Errorf("degraded line = %q", line)
	}
	noBackend := mcpMountReport{backendErr: "missing backend URL", authState: "absent"}.planesSkipped("no backend URL resolved")
	if line := mcpServeLine(noBackend); !strings.Contains(line, "degraded: no backend URL") || !strings.Contains(line, "ORUN_BACKEND_URL") {
		t.Errorf("no-backend line = %q", line)
	}
}

// TestMcpVerboseSummary: --verbose explains what mounted and why — plane,
// auth, workspace, and backend lines all present with their reasons.
func TestMcpVerboseSummary(t *testing.T) {
	rep := mcpMountReport{
		backendURL:      "https://api.example",
		authState:       "ok",
		authSource:      "session",
		authDetail:      "local session for dev@example.com",
		expiresAt:       "2026-07-12T15:04:05Z",
		workspace:       "ws_1",
		workspaceSource: "--workspace",
		workMounted:     true, workReason: "workspace ws_1 (from --workspace)",
		platformMounted: true, platformReason: "auth resolved (session)",
	}
	sum := mcpVerboseSummary(rep)
	for _, want := range []string{
		"backend URL:",
		"https://api.example",
		"auth:",
		"ok (session) — local session for dev@example.com",
		"token expiry:",
		"2026-07-12T15:04:05Z",
		"workspace:",
		"ws_1 (from --workspace)",
		"plane work:",
		"plane platform:",
		"mounted — auth resolved (session)",
		"plane server:",
		"connection_info",
	} {
		if !strings.Contains(sum, want) {
			t.Errorf("verbose summary lacks %q:\n%s", want, sum)
		}
	}

	deg := mcpVerboseSummary(degradedReport())
	for _, want := range []string{"auth:", "absent", "skipped — auth did not resolve", "none resolved (checked --workspace"} {
		if !strings.Contains(deg, want) {
			t.Errorf("degraded verbose summary lacks %q:\n%s", want, deg)
		}
	}
}

// TestBuildMcpMountReportDegraded drives the real builder with a scrubbed
// environment: no OIDC, no ORUN_TOKEN, no session on disk. With a backend
// URL it degrades to absent auth; with none it also records the backend
// miss. Neither path may produce a client — and serve therefore mounts only
// connection_info.
func TestBuildMcpMountReportDegraded(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	t.Setenv("ORUN_TOKEN", "")
	t.Setenv(backendURLEnvVar, "")
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "")
	t.Setenv("HOME", t.TempDir())

	// Bogus backend, no credentials: absent auth, both planes skipped.
	client, rep := buildMcpMountReport(context.Background(), "http://127.0.0.1:1", "")
	if client != nil {
		t.Fatal("degraded resolution must not build a client")
	}
	if rep.authState != "absent" || rep.workMounted || rep.platformMounted {
		t.Fatalf("degraded report = %+v", rep)
	}
	if !strings.Contains(rep.platformReason, "orun auth login") {
		t.Fatalf("platform skip reason must carry the fix: %q", rep.platformReason)
	}

	// No backend URL either: same degraded shape, backend miss recorded.
	client, rep = buildMcpMountReport(context.Background(), "", "")
	if client != nil || rep.backendURL != "" || rep.backendErr == "" {
		t.Fatalf("no-backend report = %+v (client=%v)", rep, client)
	}
	if rep.workMounted || rep.platformMounted {
		t.Fatalf("no-backend planes must skip: %+v", rep)
	}
	if fix := rep.connectionInfo().Fix; !strings.Contains(fix, "ORUN_BACKEND_URL") {
		t.Fatalf("no-backend fix = %q", fix)
	}
}

// TestBuildMcpMountReportStaticToken: ORUN_TOKEN + a backend URL mounts the
// platform plane (and work too when a workspace resolves) without any
// network call.
func TestBuildMcpMountReportStaticToken(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ORUN_TOKEN", "dummy")
	t.Setenv(backendURLEnvVar, "")
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "")
	t.Setenv("HOME", t.TempDir())

	client, rep := buildMcpMountReport(context.Background(), "http://127.0.0.1:1", "")
	if client == nil {
		t.Fatal("static token must build a client")
	}
	if rep.authState != "ok" || rep.authSource != "ORUN_TOKEN" {
		t.Fatalf("auth = %q/%q, want ok/ORUN_TOKEN", rep.authState, rep.authSource)
	}
	if !rep.platformMounted || rep.workMounted {
		t.Fatalf("expected platform-only mount without a workspace: %+v", rep)
	}

	client, rep = buildMcpMountReport(context.Background(), "http://127.0.0.1:1", "ws_flag")
	if client == nil || !rep.workMounted || rep.workspace != "ws_flag" || rep.workspaceSource != "--workspace" {
		t.Fatalf("flagged workspace must mount work: %+v", rep)
	}
}
