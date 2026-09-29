package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The O2 CLI seam, tested the way it runs: real subcommands executed against
// an httptest backend speaking the {data:…} envelope, auth short-circuited by
// ORUN_TOKEN, scope pinned by --workspace (no repo link involved).

func runTaskCmd(t *testing.T, cmd *cobra.Command, backend string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_TOKEN", "test-token")
	if backend != "" {
		args = append(args, "--backend-url", backend, "--workspace", "org_x")
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func envelope(t *testing.T, w http.ResponseWriter, status int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		t.Errorf("encode envelope: %v", err)
	}
}

const testTaskDoc = `apiVersion: orun.io/v1
kind: TaskContract
metadata:
  name: ENG-1
spec:
  goal: ship it
  affects: [web]
  doneWhen: [merged]
  gates: [tests]
  secrets: [STRIPE_TEST_*]
  envs: [dev]
`

func writeTestDoc(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tasks", "ENG-1.TaskContract.yaml")
	if err := os.WriteFile(path, []byte(testTaskDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	initTempGit(t, dir)
	return path
}

// initTempGit makes dir a git checkout with a non-GitHub remote: enough for
// cloudClient's repo-context detection to succeed, without any link lookup.
func initTempGit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "https://example.invalid/repo.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestTaskListRendersRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/org_x/tasks" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		envelope(t, w, 200, map[string]any{"tasks": []map[string]any{
			{"id": "tsk_3KF9TQ2P", "key": "ENG-1", "titleMirror": "Ship it", "contractHash": "sha256:" + strings.Repeat("ab", 32)},
			{"id": "tsk_3KF9TQ2Q", "key": "TSK-2"},
		}})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskListCommand(), srv.URL)
	if err != nil {
		t.Fatalf("task list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ENG-1") || !strings.Contains(out, "tsk_3KF9TQ2P") || !strings.Contains(out, "abababab") {
		t.Fatalf("list output missing rows:\n%s", out)
	}
	// A task without a contract renders "-", not an empty cell.
	if !strings.Contains(out, "TSK-2") || !strings.Contains(out, "-") {
		t.Fatalf("bare task row:\n%s", out)
	}
}

func TestTaskShowRendersVerdict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/organizations/org_x/tasks/ENG-1":
			envelope(t, w, 200, map[string]any{"task": map[string]any{
				"id": "tsk_3KF9TQ2P", "key": "ENG-1", "titleMirror": "Ship it",
				"contractHash": "sha256:" + strings.Repeat("cd", 32),
			}})
		case "/v1/organizations/org_x/tasks/ENG-1/verdict":
			envelope(t, w, 200, map[string]any{
				"verdict": map[string]any{
					"rung":     "in_review",
					"evidence": map[string]any{"observationId": "obs_1", "reason": "pr_opened observed"},
					"blocked":  true, "blockedBy": []string{"WEB-2"},
				},
				"contractHash": "sha256:" + strings.Repeat("cd", 32),
				"dependencies": []map[string]any{{"ref": "WEB-2", "state": "open"}},
			})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskShowCommand(), srv.URL, "ENG-1")
	if err != nil {
		t.Fatalf("task show: %v\n%s", err, out)
	}
	for _, want := range []string{"in_review — pr_opened observed", "blocked   by WEB-2", "dep       WEB-2 (open)", "cdcdcdcd"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskCreateAttachesRepoContract(t *testing.T) {
	writeTestDoc(t)
	var attached struct {
		Contract     map[string]any `json:"contract"`
		ContractHash string         `json:"contractHash"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/org_x/tasks":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("create body: %v", err)
			}
			if req["adoptKey"] != "ENG-1" {
				t.Errorf("adoptKey = %v", req["adoptKey"])
			}
			envelope(t, w, 201, map[string]any{"task": map[string]any{"id": "tsk_3KF9TQ2P", "key": "ENG-1"}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/organizations/org_x/tasks/ENG-1/contract":
			if err := json.NewDecoder(r.Body).Decode(&attached); err != nil {
				t.Errorf("attach body: %v", err)
			}
			envelope(t, w, 200, map[string]any{"contractHash": attached.ContractHash, "syncedAt": "2026-01-01T00:00:00Z"})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskCreateCommand(), srv.URL, "--adopt", "ENG-1")
	if err != nil {
		t.Fatalf("task create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created ENG-1 (tsk_3KF9TQ2P)") {
		t.Fatalf("create output:\n%s", out)
	}
	// The attach uploaded the sealed identity alongside the body: the server
	// verifies exactly this pairing (TK-J).
	if !strings.HasPrefix(attached.ContractHash, "sha256:") || attached.Contract["goal"] != "ship it" {
		t.Fatalf("attach carried %+v", attached)
	}
	if !strings.Contains(out, "contract "+attached.ContractHash[7:15]+" attached") {
		t.Fatalf("attach not reported:\n%s", out)
	}
}

func TestTaskCreateWithoutDocumentSaysSo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initTempGit(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope(t, w, 201, map[string]any{"task": map[string]any{"id": "tsk_3KF9TQ2P", "key": "TSK-7"}})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskCreateCommand(), srv.URL)
	if err != nil {
		t.Fatalf("task create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "created without narrowing") {
		t.Fatalf("no-document note missing:\n%s", out)
	}
}

const testTemplateDoc = `apiVersion: orun.io/v1
kind: TaskContract
spec:
  goal: D1, KV and db-migrate live on stage and prod
  affects: [infra/cloudflare-d1, infra/cloudflare-kv, infra/db-migrate]
  doneWhen: [WIRING_* secrets published for stage and prod]
  gates: []
`

// BT-O1: a task born clubbed, briefed, assigned and contracted in one
// breath — the shape a bootstrap flow needs, with a template contract that
// is not (and cannot yet be) named after the key.
func TestTaskCreateClubsAndAttachesTemplate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initTempGit(t, dir)
	tpl := filepath.Join(dir, "task-contract.yaml")
	if err := os.WriteFile(tpl, []byte(testTemplateDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	var attached struct {
		Contract     map[string]any `json:"contract"`
		ContractHash string         `json:"contractHash"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/org_x/tasks":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("create body: %v", err)
			}
			envelope(t, w, 201, map[string]any{"task": map[string]any{
				"id": "tsk_3KF9TQ2P", "key": "BASE-3", "keyOrigin": "derived",
				"epic":      map[string]any{"id": "epc_AB12CD34", "slug": "infra-baselining", "key": "EP-1"},
				"milestone": map[string]any{"id": "mls_EF56GH78", "name": "03 — infrastructure"},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/organizations/org_x/tasks/BASE-3/contract":
			if err := json.NewDecoder(r.Body).Decode(&attached); err != nil {
				t.Errorf("attach body: %v", err)
			}
			envelope(t, w, 200, map[string]any{"contractHash": attached.ContractHash, "syncedAt": "2026-01-01T00:00:00Z"})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskCreateCommand(), srv.URL,
		"--prefix", "BASE", "--title", "phase(03-infrastructure): d1, kv, db-migrate",
		"--brief", "Land the data plane.", "--epic", "infra-baselining", "--milestone", "mls_EF56GH78",
		"--assignee", "me", "--contract", tpl)
	if err != nil {
		t.Fatalf("task create: %v\n%s", err, out)
	}
	// The wire carries all five new fields verbatim (the plane's W3 create).
	for k, want := range map[string]string{
		"mintPrefix": "BASE", "titleMirror": "phase(03-infrastructure): d1, kv, db-migrate",
		"brief": "Land the data plane.", "epic": "infra-baselining", "milestone": "mls_EF56GH78", "assignee": "me",
	} {
		if created[k] != want {
			t.Errorf("create body %s = %v, want %q", k, created[k], want)
		}
	}
	// An explicit empty gate list travels as gatesDefined — the fold's
	// "merge alone finishes it" — not as a dropped array.
	if attached.Contract["gatesDefined"] != true {
		t.Fatalf("attach carried %+v — gatesDefined missing", attached.Contract)
	}
	if !strings.HasPrefix(attached.ContractHash, "sha256:") {
		t.Fatalf("attach hash %q", attached.ContractHash)
	}
	for _, want := range []string{
		"created BASE-3 (tsk_3KF9TQ2P) in milestone 03 — infrastructure",
		"attached from " + tpl + " (merge alone finishes it)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
}

func TestTaskCreateRefusesBadTemplateBeforeMinting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initTempGit(t, dir)
	tpl := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(tpl, []byte("apiVersion: orun.io/v1\nkind: TaskContract\nspec:\n  secerts: [X]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the allocator must not be reached for a malformed template: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	out, err := runTaskCmd(t, newTaskCreateCommand(), srv.URL, "--contract", tpl)
	if err == nil || !strings.Contains(err.Error(), "--contract") {
		t.Fatalf("expected a --contract error, got %v\n%s", err, out)
	}
}

func TestTaskListFiltersByEpic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/org_x/tasks" || r.URL.Query().Get("epic") != "infra-baselining" || r.URL.Query().Get("assignee") != "me" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.String())
		}
		envelope(t, w, 200, map[string]any{"tasks": []map[string]any{
			{"id": "tsk_3KF9TQ2P", "key": "BASE-3", "titleMirror": "phase(03-infrastructure)",
				"milestone": map[string]any{"id": "mls_EF56GH78", "name": "03 — infrastructure"}},
		}})
	}))
	defer srv.Close()
	out, err := runTaskCmd(t, newTaskListCommand(), srv.URL, "--epic", "infra-baselining", "--assignee", "me")
	if err != nil {
		t.Fatalf("task list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "in milestone 03 — infrastructure") {
		t.Fatalf("list output missing membership:\n%s", out)
	}
}

// BT-O2: the containers — an epic adopted on a taken slug, a phase placed
// after its sibling, and the show read with both voices.
func TestTaskEpicCreateAdoptsTakenSlug(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/org_x/tasks/epics" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("body: %v", err)
		}
		if req["slug"] != "infra-baselining" || req["owner"] != "me" || req["name"] != "Infra baselining" {
			t.Errorf("create body %v", req)
		}
		calls++
		if calls == 1 {
			envelope(t, w, 201, map[string]any{"epic": map[string]any{"id": "epc_AB12CD34", "slug": "infra-baselining", "key": "EP-1", "name": "Infra baselining"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"code": "conflict", "message": "slug is already taken in this workspace",
			"details": map[string]any{"existing": map[string]any{"id": "epc_AB12CD34", "slug": "infra-baselining", "key": "EP-1"}},
		}})
	}))
	defer srv.Close()

	args := []string{"--name", "Infra baselining", "--slug", "infra-baselining", "--owner", "me"}
	out, err := runTaskCmd(t, newTaskEpicCreateCommand(), srv.URL, args...)
	if err != nil || !strings.Contains(out, "created epic infra-baselining (EP-1, epc_AB12CD34)") {
		t.Fatalf("first create: %v\n%s", err, out)
	}
	out, err = runTaskCmd(t, newTaskEpicCreateCommand(), srv.URL, append(args, "--json")...)
	if err != nil {
		t.Fatalf("second create must adopt, got %v\n%s", err, out)
	}
	var got struct {
		Existed bool `json:"existed"`
		Epic    struct {
			ID string `json:"id"`
		} `json:"epic"`
	}
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil || !got.Existed || got.Epic.ID != "epc_AB12CD34" {
		t.Fatalf("adopt json: %v\n%s", jerr, out)
	}
}

func TestTaskEpicCreateSurfacesOtherConflicts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "conflict", "message": "something else"}})
	}))
	defer srv.Close()
	if _, err := runTaskCmd(t, newTaskEpicCreateCommand(), srv.URL, "--name", "X"); err == nil {
		t.Fatal("a conflict without a holder must stay an error")
	}
}

func TestTaskMilestoneCreatePositions(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/org_x/tasks/epics/infra-baselining/milestones" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		bodies = append(bodies, buf.String())
		envelope(t, w, 201, map[string]any{"milestone": map[string]any{"id": "mls_EF56GH78", "epicId": "epc_AB12CD34", "name": "03 — infrastructure"}})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newTaskMilestoneCreateCommand(), srv.URL,
		"--epic", "infra-baselining", "--name", "03 — infrastructure",
		"--exit-criteria", "WIRING_* published", "--exit-criteria", "convergence green", "--after", "mls_02FOUND1")
	if err != nil || !strings.Contains(out, "created milestone 03 — infrastructure (mls_EF56GH78) in epic infra-baselining") {
		t.Fatalf("milestone create: %v\n%s", err, out)
	}
	if _, err := runTaskCmd(t, newTaskMilestoneCreateCommand(), srv.URL, "--epic", "infra-baselining", "--name", "01 — scaffold", "--first"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := runTaskCmd(t, newTaskMilestoneCreateCommand(), srv.URL, "--epic", "infra-baselining", "--name", "08 — docs"); err != nil {
		t.Fatalf("last: %v", err)
	}
	// The three positions the server distinguishes: a sibling, an explicit
	// null (first), and the key absent (last).
	want := []string{`"after":"mls_02FOUND1"`, `"after":null`, ""}
	for i, w := range want {
		if w != "" && !strings.Contains(bodies[i], w) {
			t.Errorf("body %d = %s, want %s", i, bodies[i], w)
		}
		if w == "" && strings.Contains(bodies[i], `"after"`) {
			t.Errorf("body %d = %s, want no after key", i, bodies[i])
		}
	}
	if !strings.Contains(bodies[0], `"exitCriteria":["WIRING_* published","convergence green"]`) {
		t.Errorf("exit criteria: %s", bodies[0])
	}
	if _, err := runTaskCmd(t, newTaskMilestoneCreateCommand(), srv.URL, "--epic", "e", "--name", "n", "--first", "--after", "mls_X"); err == nil {
		t.Fatal("--first with --after must refuse")
	}
}

func TestTaskEpicShowRendersBothVoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/org_x/tasks/epics/infra-baselining" || r.URL.Query().Get("include") != "rollup" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.String())
		}
		envelope(t, w, 200, map[string]any{
			"epic":       map[string]any{"id": "epc_AB12CD34", "slug": "infra-baselining", "key": "EP-1", "name": "Infra baselining", "state": "Planning", "owner": "sp_1"},
			"milestones": []map[string]any{{"id": "mls_1", "name": "01 — scaffold"}, {"id": "mls_3", "name": "03 — infrastructure"}},
			"taskCount":  3,
			"rollup": map[string]any{"total": 3, "done": 1, "blocked": 0, "rungs": map[string]int{"done": 1, "in_review": 2},
				"milestones": []map[string]any{{"id": "mls_1", "name": "01 — scaffold", "total": 1, "done": 1}, {"id": "mls_3", "name": "03 — infrastructure", "total": 2, "done": 0}}},
		})
	}))
	defer srv.Close()
	out, err := runTaskCmd(t, newTaskEpicShowCommand(), srv.URL, "infra-baselining")
	if err != nil {
		t.Fatalf("epic show: %v\n%s", err, out)
	}
	for _, want := range []string{"infra-baselining  EP-1  epc_AB12CD34", "state     orun: Planning · orun: 1/3 done", "owner     sp_1", "mls_3", "0/2", "1/1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
}

func TestTaskCheckOfflineHappyPath(t *testing.T) {
	writeTestDoc(t)
	out, err := runTaskCmd(t, newTaskCheckCommand(), "", "ENG-1")
	if err != nil {
		t.Fatalf("task check: %v\n%s", err, out)
	}
	for _, want := range []string{"sealed    sha256:", "complete  yes", "advisory:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("check output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskCheckReportsIncomplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	draft := "apiVersion: orun.io/v1\nkind: TaskContract\nmetadata:\n  name: ENG-1\nspec:\n  goal: ship it\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks", "ENG-1.TaskContract.yaml"), []byte(draft), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, err := runTaskCmd(t, newTaskCheckCommand(), "", "ENG-1")
	if err != nil {
		t.Fatalf("a draft contract is legal; check errored: %v\n%s", err, out)
	}
	if !strings.Contains(out, "complete  no — missing") || !strings.Contains(out, "affects") || !strings.Contains(out, "gates") {
		t.Fatalf("incomplete report:\n%s", out)
	}
}

func TestTaskCheckWithoutDocument(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runTaskCmd(t, newTaskCheckCommand(), "", "ENG-1")
	if err != nil {
		t.Fatalf("absence is a legal state; check errored: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no contract document") || !strings.Contains(out, "no narrowing") {
		t.Fatalf("absence report:\n%s", out)
	}
}

// saas-work-gitops WG-1: with `work.tasks` declared in intent.yaml, the
// contract lookup reads the declared directory — and nothing else changes.
func TestTaskCreateReadsDeclaredTasksDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "work", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work", "tasks", "ENG-1.TaskContract.yaml"), []byte(testTaskDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\nwork:\n  tasks: work/tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	initTempGit(t, dir)
	attached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/org_x/tasks":
			envelope(t, w, 201, map[string]any{"task": map[string]any{"id": "tsk_3KF9TQ2P", "key": "ENG-1"}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/organizations/org_x/tasks/ENG-1/contract":
			attached = true
			envelope(t, w, 200, map[string]any{"contractHash": "sha256:abcdef0123", "syncedAt": "2026-01-01T00:00:00Z"})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	out, err := runTaskCmd(t, newTaskCreateCommand(), srv.URL, "--adopt", "ENG-1")
	if err != nil {
		t.Fatalf("task create: %v\n%s", err, out)
	}
	if !attached || strings.Contains(out, "no contract document") {
		t.Fatalf("declared tasks dir not read:\n%s", out)
	}
	if got := taskDocRel("ENG-1"); got != "work/tasks/ENG-1.TaskContract.yaml" {
		t.Fatalf("taskDocRel = %q", got)
	}
}
