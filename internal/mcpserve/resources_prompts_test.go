package mcpserve

// UM6 loop tests: resources/* and prompts/* handling, and the conditional
// capabilities advertisement — a tools-only composition must keep the exact
// pre-UM6 surface ({tools:{}}, -32601 on the new methods).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// rpProvider is a fakeProvider that also supplies one resource template and
// one prompt (the optional UM6 interfaces).
type rpProvider struct {
	fakeProvider
	readErr error
}

func (f *rpProvider) ResourceTemplates() []ResourceTemplateDef {
	return []ResourceTemplateDef{{
		URITemplate: "fake://{workspace}/{id}",
		Name:        "fake_resource",
		Title:       "Fake resource",
		Description: "one fake resource",
		MimeType:    "text/markdown",
	}}
}

func (f *rpProvider) ReadResource(_ context.Context, uri string) ([]ResourceContent, bool, error) {
	if !strings.HasPrefix(uri, "fake://") {
		return nil, false, nil
	}
	if f.readErr != nil {
		return nil, true, f.readErr
	}
	return []ResourceContent{{URI: uri, MimeType: "text/markdown", Text: "# fake\n" + uri}}, true, nil
}

func (f *rpProvider) Prompts() []PromptDef {
	return []PromptDef{{
		Name:        "fake_prompt",
		Description: "one fake prompt",
		Arguments: []PromptArg{
			{Name: "workspace", Description: "required arg", Required: true},
			{Name: "project", Description: "optional arg", Required: false},
		},
	}}
}

func (f *rpProvider) RenderPrompt(name string, args map[string]string) (string, bool) {
	if name != "fake_prompt" {
		return "", false
	}
	return "prompt for " + args["workspace"], true
}

func errorField(t *testing.T, resp map[string]interface{}) (float64, string) {
	t.Helper()
	e, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("no error in %v", resp)
	}
	code, _ := e["code"].(float64)
	msg, _ := e["message"].(string)
	return code, msg
}

// TestCapabilitiesConditional: a composition where no provider supplies
// resources/prompts advertises tools only and -32601s the new methods; a
// composition with a supplying provider advertises all three.
func TestCapabilitiesConditional(t *testing.T) {
	toolsOnly, _, _ := twoProviderServer()
	responses := rpc(t, toolsOnly,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/templates/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"fake://a/b"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"fake_prompt"}}`,
	)
	caps := responses[0]["result"].(map[string]interface{})["capabilities"].(map[string]interface{})
	if _, ok := caps["tools"]; !ok {
		t.Fatalf("capabilities lack tools: %v", caps)
	}
	if _, ok := caps["resources"]; ok {
		t.Fatalf("tools-only serve must not advertise resources: %v", caps)
	}
	if _, ok := caps["prompts"]; ok {
		t.Fatalf("tools-only serve must not advertise prompts: %v", caps)
	}
	for i := 1; i < 6; i++ {
		if code, _ := errorField(t, responses[i]); code != -32601 {
			t.Errorf("unadvertised method %d: code = %v, want -32601", i, code)
		}
	}

	full := &Server{Providers: []ToolProvider{&rpProvider{fakeProvider: fakeProvider{id: "p", names: []string{"one_tool"}}}}, Version: "test"}
	responses = rpc(t, full, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	caps = responses[0]["result"].(map[string]interface{})["capabilities"].(map[string]interface{})
	for _, key := range []string{"tools", "resources", "prompts"} {
		if _, ok := caps[key]; !ok {
			t.Errorf("full serve capabilities lack %s: %v", key, caps)
		}
	}
}

func rpServer() *Server {
	return &Server{Providers: []ToolProvider{&rpProvider{fakeProvider: fakeProvider{id: "p", names: []string{"one_tool"}}}}, Version: "test"}
}

func TestResourcesProtocol(t *testing.T) {
	s := rpServer()
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/templates/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"fake://ws/x"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"other://ws/x"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{}}`,
	)

	// resources/list is always empty: templates only (the TS posture).
	list := responses[0]["result"].(map[string]interface{})["resources"].([]interface{})
	if len(list) != 0 {
		t.Fatalf("resources/list = %v, want empty", list)
	}

	templates := responses[1]["result"].(map[string]interface{})["resourceTemplates"].([]interface{})
	if len(templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(templates))
	}
	tpl := templates[0].(map[string]interface{})
	if tpl["uriTemplate"] != "fake://{workspace}/{id}" || tpl["name"] != "fake_resource" || tpl["mimeType"] != "text/markdown" {
		t.Fatalf("template shape: %v", tpl)
	}

	contents := responses[2]["result"].(map[string]interface{})["contents"].([]interface{})
	block := contents[0].(map[string]interface{})
	if block["uri"] != "fake://ws/x" || block["mimeType"] != "text/markdown" || !strings.Contains(block["text"].(string), "# fake") {
		t.Fatalf("read contents: %v", block)
	}

	// A uri no template matches is a protocol error, not an isError result.
	if code, msg := errorField(t, responses[3]); code != -32002 || !strings.Contains(msg, "other://ws/x") {
		t.Fatalf("unmatched uri: code=%v msg=%q", code, msg)
	}
	if code, _ := errorField(t, responses[4]); code != -32602 {
		t.Fatalf("missing uri: code=%v, want -32602", code)
	}
}

// TestResourceReadErrorIsProtocolError: an owned read failure has no isError
// channel — it surfaces as a -32603 protocol error carrying the provider's
// coded message.
func TestResourceReadErrorIsProtocolError(t *testing.T) {
	s := &Server{Providers: []ToolProvider{&rpProvider{
		fakeProvider: fakeProvider{id: "p", names: []string{"one_tool"}},
		readErr:      errors.New("forbidden: missing member role (requestId: req_9)"),
	}}, Version: "test"}
	responses := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"fake://ws/x"}}`)
	code, msg := errorField(t, responses[0])
	if code != -32603 || !strings.Contains(msg, "forbidden: missing member role") {
		t.Fatalf("read error: code=%v msg=%q", code, msg)
	}
}

func TestPromptsProtocol(t *testing.T) {
	s := rpServer()
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"fake_prompt","arguments":{"workspace":"acme"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"fake_prompt","arguments":{"project":"prj1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"no_such_prompt","arguments":{}}}`,
	)

	prompts := responses[0]["result"].(map[string]interface{})["prompts"].([]interface{})
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(prompts))
	}
	def := prompts[0].(map[string]interface{})
	if def["name"] != "fake_prompt" {
		t.Fatalf("prompt def: %v", def)
	}
	args := def["arguments"].([]interface{})
	if len(args) != 2 || args[0].(map[string]interface{})["required"] != true || args[1].(map[string]interface{})["required"] != false {
		t.Fatalf("argument required flags: %v", args)
	}

	// prompts/get renders the single user-role text message (the TS SDK's
	// output shape).
	result := responses[1]["result"].(map[string]interface{})
	messages := result["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	msg := messages[0].(map[string]interface{})
	content := msg["content"].(map[string]interface{})
	if msg["role"] != "user" || content["type"] != "text" || content["text"] != "prompt for acme" {
		t.Fatalf("message shape: %v", msg)
	}

	// Missing required argument and unknown prompt are -32602 protocol errors.
	if code, emsg := errorField(t, responses[2]); code != -32602 || !strings.Contains(emsg, `"workspace"`) {
		t.Fatalf("missing required arg: code=%v msg=%q", code, emsg)
	}
	if code, emsg := errorField(t, responses[3]); code != -32602 || !strings.Contains(emsg, "no_such_prompt") {
		t.Fatalf("unknown prompt: code=%v msg=%q", code, emsg)
	}
}
