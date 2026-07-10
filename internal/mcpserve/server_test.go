package mcpserve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeProvider is a minimal ToolProvider: it owns its listed names and
// answers each call with "<provider>:<tool>".
type fakeProvider struct {
	id    string
	names []string
	calls []string
}

func (f *fakeProvider) Tools() []ToolDef {
	defs := []ToolDef{}
	for _, n := range f.names {
		defs = append(defs, ToolDef{Name: n, Description: "fake " + n, InputSchema: map[string]interface{}{"type": "object"}})
	}
	return defs
}

func (f *fakeProvider) Call(_ context.Context, name string, _ json.RawMessage) (Result, bool) {
	for _, n := range f.names {
		if n == name {
			f.calls = append(f.calls, name)
			return TextResult(f.id+":"+name, false), true
		}
	}
	return nil, false
}

func rpc(t *testing.T, s *Server, lines ...string) []map[string]interface{} {
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

func callLine(id int, tool string, args string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"%s","arguments":%s}}`, id, tool, args)
}

func resultText(t *testing.T, resp map[string]interface{}) (string, bool) {
	t.Helper()
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	isErr, _ := result["isError"].(bool)
	return text, isErr
}

func twoProviderServer() (*Server, *fakeProvider, *fakeProvider) {
	work := &fakeProvider{id: "work", names: []string{"work_query", "task_create"}}
	platform := &fakeProvider{id: "platform", names: []string{"runs_list", "audit_search"}}
	return &Server{Providers: []ToolProvider{work, platform}, Version: "9.9.9-test"}, work, platform
}

func TestInitializeServerInfoAndNotificationSilence(t *testing.T) {
	s, _, _ := twoProviderServer()
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d (notification must get none)", len(responses))
	}
	init := responses[0]["result"].(map[string]interface{})
	if init["protocolVersion"] != ProtocolVersion {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}
	if _, ok := init["capabilities"].(map[string]interface{})["tools"]; !ok {
		t.Fatalf("capabilities lack tools: %v", init["capabilities"])
	}
	// serverInfo is the binary's identity: name "orun", the build version.
	info := init["serverInfo"].(map[string]interface{})
	if info["name"] != "orun" || info["version"] != "9.9.9-test" {
		t.Fatalf("serverInfo = %v, want {orun 9.9.9-test}", info)
	}
	if ping := responses[1]["result"].(map[string]interface{}); len(ping) != 0 {
		t.Fatalf("ping result = %v, want empty object", ping)
	}
}

func TestMergedListAndRoutedCalls(t *testing.T) {
	s, work, platform := twoProviderServer()
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		callLine(2, "work_query", `{}`),
		callLine(3, "runs_list", `{}`),
		callLine(4, "no_such_tool", `{}`),
	)

	// tools/list merges rosters in Providers order.
	tools := responses[0]["result"].(map[string]interface{})["tools"].([]interface{})
	var names []string
	for _, tl := range tools {
		names = append(names, tl.(map[string]interface{})["name"].(string))
	}
	want := []string{"work_query", "task_create", "runs_list", "audit_search"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("merged roster = %v, want %v", names, want)
	}
	// The forbidden-tool sweep (WP-3/WP-10, README locked decision 5) is
	// reusable over any composed roster via the shared fragments.
	for _, name := range names {
		for _, frag := range ForbiddenNameFragments {
			if strings.Contains(name, frag) {
				t.Errorf("forbidden tool on the merged surface: %s", name)
			}
		}
	}

	// tools/call routes to the owning provider, in order.
	if text, isErr := resultText(t, responses[1]); isErr || text != "work:work_query" {
		t.Fatalf("work_query routed wrong: %q (isError=%v)", text, isErr)
	}
	if text, isErr := resultText(t, responses[2]); isErr || text != "platform:runs_list" {
		t.Fatalf("runs_list routed wrong: %q (isError=%v)", text, isErr)
	}
	if len(work.calls) != 1 || len(platform.calls) != 1 {
		t.Fatalf("calls = work %v, platform %v", work.calls, platform.calls)
	}

	// A name no provider owns keeps the work MCP's historical shape: an
	// isError result, never a protocol fault.
	text, isErr := resultText(t, responses[3])
	if !isErr || text != "error: unknown tool no_such_tool" {
		t.Fatalf("unknown tool shape: %q (isError=%v)", text, isErr)
	}
}

func TestDuplicateToolNameGuard(t *testing.T) {
	a := &fakeProvider{id: "a", names: []string{"runs_list"}}
	b := &fakeProvider{id: "b", names: []string{"runs_list"}}
	s := &Server{Providers: []ToolProvider{a, b}, Version: "test"}
	err := s.Serve(context.Background(), strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), `duplicate tool name "runs_list"`) {
		t.Fatalf("duplicate roster must fail loudly at serve start, got %v", err)
	}
	if len(a.calls)+len(b.calls) != 0 {
		t.Fatalf("no call may run on a shadowed roster")
	}
}

func TestProtocolErrors(t *testing.T) {
	s, _, _ := twoProviderServer()
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"no/such"}`,
		`not json at all`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"bogus"}`,
	)
	if code := responses[0]["error"].(map[string]interface{})["code"].(float64); code != -32601 {
		t.Fatalf("unknown method code = %v", code)
	}
	if code := responses[1]["error"].(map[string]interface{})["code"].(float64); code != -32700 {
		t.Fatalf("parse error code = %v", code)
	}
	if code := responses[2]["error"].(map[string]interface{})["code"].(float64); code != -32602 {
		t.Fatalf("invalid params code = %v", code)
	}
}

func TestLineLimits(t *testing.T) {
	s, _, _ := twoProviderServer()

	// A line past the 64 KiB initial buffer (but under 16 MiB) must serve.
	pad := strings.Repeat("a", 256*1024)
	responses := rpc(t, s, callLine(1, "work_query", `{"pad":"`+pad+`"}`))
	if text, isErr := resultText(t, responses[0]); isErr || text != "work:work_query" {
		t.Fatalf("large-line call failed: %q (isError=%v)", text, isErr)
	}

	// A line past the 16 MiB scanner cap surfaces the scanner error,
	// exactly as the workmcp loop always did.
	over := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("a", 16*1024*1024) + `"}}` + "\n"
	err := s.Serve(context.Background(), strings.NewReader(over), &strings.Builder{})
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("oversized line: err = %v, want bufio.ErrTooLong", err)
	}
}
