package platformmcp

import (
	"strings"
	"testing"
)

// checkAutoKey asserts an auto-minted Idempotency-Key's shape: mcp_ + UUID,
// ≤ 255 printable ASCII.
func checkAutoKey(t *testing.T, key string) {
	t.Helper()
	if !strings.HasPrefix(key, "mcp_") {
		t.Errorf("auto key %q lacks the mcp_ prefix", key)
	}
	if len(key) != len("mcp_")+36 {
		t.Errorf("auto key %q is not mcp_ + a 36-char UUID", key)
	}
	if !validIdemKey(key) {
		t.Errorf("auto key %q is not 1-255 printable ASCII", key)
	}
}

// TestPerWriteToolHappyPath drives every write tool over the fake seam: the
// right method with the right body, an auto Idempotency-Key present, and the
// summary + compact-JSON output shape.
func TestPerWriteToolHappyPath(t *testing.T) {
	cases := []struct {
		tool, args  string
		wantCalls   []string
		wantWrites  int // how many entries of wantCalls carry a key
		summaryFrag string
	}{
		{"project_create", `{"workspace":"ws_1","name":"api","slug":"api"}`,
			[]string{`CreateProject org=ws_1 body={"name":"api","slug":"api"}`}, 1, "created project api in ws_1"},
		{"environment_create", `{"workspace":"ws_1","project":"prj_a","name":"staging"}`,
			[]string{`CreateProjectEnvironment org=ws_1 project=prj_a body={"name":"staging"}`}, 1, "created environment staging"},
		{"flag_set", `{"workspace":"ws_1","flagKey":"checkout.new_flow","enabled":true}`,
			[]string{`ListFeatureFlags org=ws_1 project= env=`,
				`CreateFeatureFlag org=ws_1 project= env= body={"enabled":true,"flagKey":"checkout.new_flow"}`}, 1, "created flag checkout.new_flow at organization scope"},
		{"webhook_create", `{"workspace":"ws_1","url":"https://example.com/hook","name":"ci"}`,
			[]string{`CreateWebhookEndpoint org=ws_1 project= body={"name":"ci","url":"https://example.com/hook"}`}, 1, "created webhook endpoint in ws_1"},
		{"webhook_create", `{"workspace":"ws_1","project":"prj_a","url":"https://example.com/hook"}`,
			[]string{`CreateWebhookEndpoint org=ws_1 project=prj_a body={"url":"https://example.com/hook"}`}, 1, "created webhook endpoint"},
		{"webhook_delivery_replay", `{"workspace":"ws_1","delivery":"wha_1"}`,
			[]string{`ReplayWebhookDelivery org=ws_1 attempt=wha_1`}, 1, "replayed webhook delivery wha_1"},
		{"member_invite", `{"workspace":"ws_1","email":"dev@example.com","role":"viewer"}`,
			[]string{`CreateInvitation org=ws_1 body={"email":"dev@example.com","role":"viewer"}`}, 1, "invited dev@example.com to ws_1 as viewer"},
	}
	for _, tc := range cases {
		api := &fakeAPI{page: page(`{"id":"obj_1"}`, "")}
		p := granted(&Provider{API: api}, "ws_1")
		text, isErr := callTool(t, p, tc.tool, tc.args)
		if isErr {
			t.Errorf("%s %s errored: %s", tc.tool, tc.args, text)
			continue
		}
		if len(api.calls) != len(tc.wantCalls) {
			t.Errorf("%s: calls = %v, want %v", tc.tool, api.calls, tc.wantCalls)
			continue
		}
		for i, want := range tc.wantCalls {
			if api.calls[i] != want {
				t.Errorf("%s: call %d = %q, want %q", tc.tool, i, api.calls[i], want)
			}
		}
		if len(api.keys) != tc.wantWrites {
			t.Errorf("%s: %d writes carried a key, want %d", tc.tool, len(api.keys), tc.wantWrites)
			continue
		}
		for _, key := range api.keys {
			checkAutoKey(t, key)
		}
		lines := strings.SplitN(text, "\n", 2)
		if len(lines) != 2 || !strings.Contains(lines[0], tc.summaryFrag) {
			t.Errorf("%s: output %q lacks summary %q", tc.tool, text, tc.summaryFrag)
		}
	}
}

// TestIdempotencyKeyRails: a supplied key reaches the wire verbatim, two auto
// attempts mint two distinct keys, and an invalid supplied key is a
// validation_failed verdict before any wire call.
func TestIdempotencyKeyRails(t *testing.T) {
	api := &fakeAPI{page: page(`{}`, "")}
	p := granted(&Provider{API: api}, "ws_1")

	callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api","idempotencyKey":"retry-me-42"}`)
	if len(api.keys) != 1 || api.keys[0] != "retry-me-42" {
		t.Fatalf("supplied key not passed through verbatim: %v", api.keys)
	}

	api.keys = nil
	callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api"}`)
	callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api"}`)
	if len(api.keys) != 2 {
		t.Fatalf("keys = %v", api.keys)
	}
	if api.keys[0] == api.keys[1] {
		t.Fatalf("two logical attempts minted the same auto key: %q", api.keys[0])
	}
	checkAutoKey(t, api.keys[0])
	checkAutoKey(t, api.keys[1])

	for _, bad := range []string{`""`, `"has\nnewline"`, `"` + strings.Repeat("x", 256) + `"`, `"héllo"`} {
		api.calls, api.keys = nil, nil
		text, isErr := callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api","idempotencyKey":`+bad+`}`)
		if !isErr || !strings.Contains(text, "validation_failed") {
			t.Errorf("invalid key %s: %q (isError=%v)", bad, text, isErr)
		}
		if len(api.calls) != 0 {
			t.Errorf("invalid key %s reached the seam: %v", bad, api.calls)
		}
	}
	// 255 printable ASCII chars is the inclusive maximum.
	api.keys = nil
	max := strings.Repeat("k", 255)
	if _, isErr := callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api","idempotencyKey":"`+max+`"}`); isErr {
		t.Fatal("255-char key must be accepted")
	}
	if len(api.keys) != 1 || api.keys[0] != max {
		t.Fatalf("max-length key not passed through")
	}
}

// TestFlagSetBranches: the upsert lists the exact scope, PATCHes a matching
// flag, POSTs a new one otherwise, and rejects a call with neither enabled
// nor value.
func TestFlagSetBranches(t *testing.T) {
	// Update: the scope list carries the flag → PATCH by its id.
	api := &fakeAPI{page: page(`{"flags":[{"id":"ff_9","key":"checkout.new_flow","enabled":false}]}`, "")}
	p := granted(&Provider{API: api}, "ws_1")
	text, isErr := callTool(t, p, "flag_set",
		`{"workspace":"ws_1","project":"prj_a","flagKey":"checkout.new_flow","enabled":true,"value":{"pct":10}}`)
	if isErr {
		t.Fatalf("flag_set update errored: %s", text)
	}
	want := []string{
		`ListFeatureFlags org=ws_1 project=prj_a env=`,
		`UpdateFeatureFlag org=ws_1 project=prj_a env= flag=ff_9 body={"enabled":true,"value":{"pct":10}}`,
	}
	for i, w := range want {
		if api.calls[i] != w {
			t.Fatalf("update branch call %d = %q, want %q", i, api.calls[i], w)
		}
	}
	if !strings.Contains(text, "updated flag checkout.new_flow at project scope") {
		t.Fatalf("update summary: %q", text)
	}

	// Create: no key match at the scope → POST with flagKey in the body
	// (the contracts' CreateFeatureFlag body shape).
	api = &fakeAPI{page: page(`{"flags":[{"id":"ff_1","key":"other.flag"}]}`, "")}
	p = granted(&Provider{API: api}, "ws_1")
	text, isErr = callTool(t, p, "flag_set", `{"workspace":"ws_1","flagKey":"checkout.new_flow","value":"on"}`)
	if isErr {
		t.Fatalf("flag_set create errored: %s", text)
	}
	if api.calls[1] != `CreateFeatureFlag org=ws_1 project= env= body={"flagKey":"checkout.new_flow","value":"on"}` {
		t.Fatalf("create branch call = %q", api.calls[1])
	}

	// Neither enabled nor value → validation_failed, before any wire call.
	api = &fakeAPI{page: page(`{}`, "")}
	p = granted(&Provider{API: api}, "ws_1")
	text, isErr = callTool(t, p, "flag_set", `{"workspace":"ws_1","flagKey":"checkout.new_flow"}`)
	if !isErr || !strings.Contains(text, "validation_failed") || !strings.Contains(text, "at least one of enabled or value") {
		t.Fatalf("neither-arg verdict: %q (isError=%v)", text, isErr)
	}
	if len(api.calls) != 0 {
		t.Fatalf("neither-arg call reached the seam: %v", api.calls)
	}
	// enabled:false counts as provided (only absence fails).
	if _, isErr := callTool(t, p, "flag_set", `{"workspace":"ws_1","flagKey":"k","enabled":false}`); isErr {
		t.Fatal("enabled:false must satisfy the at-least-one rule")
	}
}

// TestWebhookFanout: events[] fans out to one subscription create per event
// under keys derived deterministically from the base (`<base>:sub<i>`) — a
// retry with the same base key replays identical sub-keys.
func TestWebhookFanout(t *testing.T) {
	api := &fakeAPI{page: page(`{"id":"whep_1"}`, "")}
	p := granted(&Provider{API: api}, "ws_1")
	text, isErr := callTool(t, p, "webhook_create",
		`{"workspace":"ws_1","url":"https://example.com/hook","events":["run.completed","run.failed"],"idempotencyKey":"base-key"}`)
	if isErr {
		t.Fatalf("webhook_create errored: %s", text)
	}
	wantCalls := []string{
		`CreateWebhookEndpoint org=ws_1 project= body={"url":"https://example.com/hook"}`,
		`CreateWebhookSubscription org=ws_1 body={"endpointId":"whep_1","eventType":"run.completed"}`,
		`CreateWebhookSubscription org=ws_1 body={"endpointId":"whep_1","eventType":"run.failed"}`,
	}
	for i, w := range wantCalls {
		if api.calls[i] != w {
			t.Fatalf("fan-out call %d = %q, want %q", i, api.calls[i], w)
		}
	}
	wantKeys := []string{"base-key", "base-key:sub0", "base-key:sub1"}
	for i, w := range wantKeys {
		if api.keys[i] != w {
			t.Fatalf("fan-out key %d = %q, want %q", i, api.keys[i], w)
		}
	}
	if !strings.Contains(text, "with 2 subscriptions") {
		t.Fatalf("fan-out summary: %q", text)
	}

	// A retry with the same base key replays byte-identical sub-keys.
	firstKeys := append([]string(nil), api.keys...)
	api.calls, api.keys = nil, nil
	callTool(t, p, "webhook_create",
		`{"workspace":"ws_1","url":"https://example.com/hook","events":["run.completed","run.failed"],"idempotencyKey":"base-key"}`)
	for i, w := range firstKeys {
		if api.keys[i] != w {
			t.Fatalf("retry key %d = %q, want %q (derived keys must be deterministic)", i, api.keys[i], w)
		}
	}

	// Auto base key: the sub-keys still derive from the endpoint's key.
	api.calls, api.keys = nil, nil
	callTool(t, p, "webhook_create", `{"workspace":"ws_1","url":"https://example.com/hook","events":["run.completed"]}`)
	checkAutoKey(t, api.keys[0])
	if api.keys[1] != api.keys[0]+":sub0" {
		t.Fatalf("auto-key derivation: base %q, sub %q", api.keys[0], api.keys[1])
	}
}

// TestMemberInviteStripsToken: the one-time accept token never reaches tool
// output — not at the top level, not in the delivery.token shape (the TS
// guard, test-pinned).
func TestMemberInviteStripsToken(t *testing.T) {
	api := &fakeAPI{page: page(`{"id":"inv_1","email":"dev@example.com","delivery":{"channel":"email","token":"SECRET-ACCEPT-TOKEN"},"token":"ALSO-SECRET"}`, "")}
	p := granted(&Provider{API: api}, "ws_1")
	text, isErr := callTool(t, p, "member_invite", `{"workspace":"ws_1","email":"dev@example.com","role":"viewer"}`)
	if isErr {
		t.Fatalf("member_invite errored: %s", text)
	}
	if strings.Contains(text, "SECRET-ACCEPT-TOKEN") || strings.Contains(text, "ALSO-SECRET") || strings.Contains(text, `"token"`) {
		t.Fatalf("accept token leaked into tool output: %s", text)
	}
	// The rest of the payload survives the strip.
	if !strings.Contains(text, `"id":"inv_1"`) || !strings.Contains(text, `"channel":"email"`) {
		t.Fatalf("non-token payload lost in the strip: %s", text)
	}
}
