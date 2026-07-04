package workmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

type fakeAPI struct {
	summary  *remotestate.WorkSummary
	comments []string
	created  []remotestate.CreateWorkTaskRequest
	assigned []string
	edited   []string
	failNext error
}

func (f *fakeAPI) GetWorkSummary(context.Context) (*remotestate.WorkSummary, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return f.summary, nil
}
func (f *fakeAPI) CreateWorkTask(_ context.Context, req remotestate.CreateWorkTaskRequest) (*remotestate.WorkMutationResponse, error) {
	f.created = append(f.created, req)
	return &remotestate.WorkMutationResponse{Key: "ORN-9", Seq: 12}, nil
}
func (f *fakeAPI) CommentWork(_ context.Context, key, body string) (*remotestate.WorkMutationResponse, error) {
	f.comments = append(f.comments, key+": "+body)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 13}, nil
}
func (f *fakeAPI) AssignWork(_ context.Context, key, subject string, _ bool) (*remotestate.WorkMutationResponse, error) {
	f.assigned = append(f.assigned, key+"→"+subject)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 14}, nil
}
func (f *fakeAPI) EditWorkContract(_ context.Context, key string, _ remotestate.WorkContract) (*remotestate.WorkMutationResponse, error) {
	f.edited = append(f.edited, key)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 15}, nil
}

func fixtureSummary() *remotestate.WorkSummary {
	return &remotestate.WorkSummary{
		Specs: []remotestate.WorkSpecView{{Key: "demo-epic", Title: "Demo", CreatedBy: remotestate.WorkActor{Type: "user", ID: "u"}, Progress: map[string]int{"ready": 1}}},
		Tasks: []remotestate.WorkTaskView{{
			Key: "ORN-1", Spec: "demo-epic", Title: "route reads",
			Contract:  &remotestate.WorkContract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}},
			CreatedBy: remotestate.WorkActor{Type: "user", ID: "u"},
			Lifecycle: remotestate.WorkLifecycle{Rung: "in_review", Evidence: []string{"PR o/r#1 open"}},
		}},
		CoordSeq: 5, ObsSeq: 3,
	}
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

func TestInitializeAndToolSurface(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d (notification must get none)", len(responses))
	}
	init := responses[0]["result"].(map[string]interface{})
	if init["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}

	tools := responses[1]["result"].(map[string]interface{})["tools"].([]interface{})
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]interface{})["name"].(string)] = true
	}
	for _, want := range []string{"work_query", "work_get", "spec_get", "task_create", "task_comment", "task_assign", "contract_propose"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if len(tools) != 7 {
		t.Errorf("tool surface = %d tools, want exactly 7 (closed)", len(tools))
	}
	// The lie is unrepresentable: no lifecycle write, no pin (WP-3, WP-10).
	for name := range names {
		if strings.Contains(name, "status") || strings.Contains(name, "pin") || strings.Contains(name, "lifecycle") {
			t.Errorf("forbidden tool on the surface: %s", name)
		}
	}
}

func TestReadsCarryEvidence(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "work_query", `{}`),
		callLine(2, "work_get", `{"key":"ORN-1"}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "PR o/r#1 open") {
		t.Fatalf("work_query lacks evidence: %s", text)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, `"rung": "in_review"`) {
		t.Fatalf("work_get lacks the derived rung: %s", text)
	}
}

func TestSpecGetSealsIntentOnly(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s, callLine(1, "spec_get", `{"spec":"demo-epic"}`))
	text, isErr := resultText(t, responses[0])
	if isErr {
		t.Fatalf("spec_get failed: %s", text)
	}
	if !strings.HasPrefix(text, "sha256:") {
		t.Fatalf("spec_get does not lead with the content id: %s", text[:40])
	}
	if strings.Contains(text, "in_review") || strings.Contains(text, "evidence") {
		t.Fatal("sealed brief leaked fold output")
	}
	if !strings.Contains(text, `"goal":"g"`) {
		t.Fatal("sealed brief lacks the contract")
	}
}

func TestWritesGoThroughTheMutators(t *testing.T) {
	api := &fakeAPI{summary: fixtureSummary()}
	s := &Server{API: api, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "task_create", `{"prefix":"ORN","title":"follow-up","spec":"demo-epic"}`),
		callLine(2, "task_comment", `{"key":"ORN-1","body":"on it"}`),
		callLine(3, "task_assign", `{"key":"ORN-1","subject":"sp_agent"}`),
		callLine(4, "contract_propose", `{"key":"ORN-1","contract":{"goal":"g2"}}`),
	)
	for i, r := range responses {
		if _, isErr := resultText(t, r); isErr {
			t.Fatalf("write %d errored: %v", i+1, r)
		}
	}
	if len(api.created) != 1 || api.created[0].Title != "follow-up" {
		t.Fatalf("created = %+v", api.created)
	}
	if len(api.assigned) != 1 || api.assigned[0] != "ORN-1→sp_agent" {
		t.Fatalf("assigned = %v", api.assigned)
	}
	if len(api.edited) != 1 {
		t.Fatalf("edited = %v", api.edited)
	}
	// contract_propose flags for human review (comment beside the edit)
	flagged := false
	for _, c := range api.comments {
		if strings.Contains(c, "human review requested") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("proposal not flagged: %v", api.comments)
	}
}

func TestErrorShapes(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary(), failNext: fmt.Errorf("backend down")}, Workspace: "ws_1"}
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"no/such"}`,
		callLine(2, "work_query", `{}`),
		callLine(3, "no_such_tool", `{}`),
	)
	errObj := responses[0]["error"].(map[string]interface{})
	if errObj["code"].(float64) != -32601 {
		t.Fatalf("unknown method code = %v", errObj["code"])
	}
	// Tool failures are results with isError (verdicts to reason about),
	// never protocol faults.
	text, isErr := resultText(t, responses[1])
	if !isErr || !strings.Contains(text, "backend down") {
		t.Fatalf("tool failure shape: %s (isError=%v)", text, isErr)
	}
	text, isErr = resultText(t, responses[2])
	if !isErr || !strings.Contains(text, "unknown tool") {
		t.Fatalf("unknown tool shape: %s", text)
	}
}
