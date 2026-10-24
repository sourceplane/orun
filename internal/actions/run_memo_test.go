package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/remotestate"
)

// planeWorld is the task plane as the server keeps it — milestones appended
// in order, keys minted BASE-1, BASE-2, … — counting every call, so a test
// can say how many round trips a bootstrap's bookkeeping costs.
type planeWorld struct {
	epic     remotestate.EpicView
	tasks    []remotestate.PublicTask
	attached map[string]string
	calls    map[string]int
}

func newPlaneWorld() *planeWorld {
	return &planeWorld{attached: map[string]string{}, calls: map[string]int{}}
}

func (w *planeWorld) CreateEpic(_ context.Context, _ string, req remotestate.EpicCreateRequest) (*remotestate.PublicEpic, error) {
	w.calls["CreateEpic"]++
	w.epic.Epic = remotestate.PublicEpic{ID: "epc_1", Slug: req.Slug, Key: "EP-1", Name: req.Name}
	return &w.epic.Epic, nil
}

func (w *planeWorld) GetEpic(_ context.Context, _, ref string) (*remotestate.EpicView, error) {
	w.calls["GetEpic"]++
	if ref != w.epic.Epic.Slug {
		return nil, fmt.Errorf("no epic %q", ref)
	}
	v := copyEpicView(w.epic)
	return &v, nil
}

func (w *planeWorld) CreateMilestone(_ context.Context, _, _ string, req remotestate.MilestoneCreateRequest) (*remotestate.PublicMilestone, error) {
	w.calls["CreateMilestone"]++
	m := remotestate.PublicMilestone{ID: fmt.Sprintf("mls_%d", len(w.epic.Milestones)+1), Name: req.Name}
	w.epic.Milestones = append(w.epic.Milestones, m)
	return &m, nil
}

func (w *planeWorld) ListTasksWhere(_ context.Context, _ string, _ remotestate.TaskListFilter) (*remotestate.TasksList, error) {
	w.calls["ListTasksWhere"]++
	return &remotestate.TasksList{Tasks: append([]remotestate.PublicTask(nil), w.tasks...)}, nil
}

func (w *planeWorld) CreateTask(_ context.Context, _ string, req remotestate.TaskCreateRequest) (*remotestate.PublicTask, error) {
	w.calls["CreateTask"]++
	t := remotestate.PublicTask{ID: fmt.Sprintf("tsk_%d", len(w.tasks)+1), Key: fmt.Sprintf("BASE-%d", len(w.tasks)+1), TitleMirror: req.TitleMirror}
	w.tasks = append(w.tasks, t)
	return &t, nil
}

func (w *planeWorld) AttachTaskContract(_ context.Context, _, keyOrID string, _ json.RawMessage, hash string) (*remotestate.TaskContractSeal, error) {
	w.calls["AttachTaskContract"]++
	w.attached[keyOrID] = hash
	return &remotestate.TaskContractSeal{ContractHash: hash}, nil
}

// bootstrapRegistration is turso-starter's preInstantiate list, then phase
// 01's own `pre` hook finding its task again.
func bootstrapRegistration(t *testing.T, plane taskPlane) []Result {
	t.Helper()
	base := writeContract(t, "tasks/01.TaskContract.yaml", contractDoc)
	writeContractInto(t, base, "tasks/02.TaskContract.yaml", contractDoc)
	hooks := []map[string]any{
		{"kind": "epic", "slug": "todos", "name": "Todos baselining"},
		{"kind": "milestone", "epic": "todos", "name": "01-product"},
		{"kind": "milestone", "epic": "todos", "name": "02-docs"},
		{"kind": "task", "epic": "todos", "milestone": "01-product", "name": "Build the product", "prefix": "BASE", "contract": "tasks/01.TaskContract.yaml"},
		{"kind": "task", "epic": "todos", "milestone": "02-docs", "name": "Record the deployment", "prefix": "BASE", "contract": "tasks/02.TaskContract.yaml"},
		{"kind": "task", "epic": "todos", "milestone": "01-product", "name": "Build the product", "prefix": "BASE", "contract": "tasks/01.TaskContract.yaml"},
	}
	var out []Result
	for _, with := range hooks {
		resolved, err := Resolve("orun.task/ensure@v1", with)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		res, err := ensureOn(context.Background(), plane, "ws_1", Input{Dir: t.TempDir(), BaseDir: base, Params: resolved})
		if err != nil {
			t.Fatalf("ensure %v: %v", with, err)
		}
		out = append(out, res)
	}
	return out
}

func writeContractInto(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunMemoCutsABootstrapsTaskRoundTrips(t *testing.T) {
	bare := newPlaneWorld()
	bareResults := bootstrapRegistration(t, bare)

	memo := newPlaneWorld()
	memoResults := bootstrapRegistration(t, memoTaskPlane{taskPlane: memo, memo: newRunMemo()})

	total := func(c map[string]int) int {
		n := 0
		for _, v := range c {
			n += v
		}
		return n
	}
	if got, was := total(memo.calls), total(bare.calls); got != 9 || was != 15 {
		t.Errorf("round trips: %d with the memo (want 9), %d without (want 15): %v vs %v", got, was, memo.calls, bare.calls)
	}
	// The memo changes how often a hook asks, never what it decides.
	for i := range bareResults {
		if fmt.Sprint(bareResults[i].Outputs) != fmt.Sprint(memoResults[i].Outputs) {
			t.Errorf("hook %d decided differently: %v without the memo, %v with it", i, bareResults[i].Outputs, memoResults[i].Outputs)
		}
	}
	if memo.calls["CreateTask"] != 2 || memo.calls["CreateMilestone"] != 2 {
		t.Errorf("each task and milestone is made once: %v", memo.calls)
	}
	if memoResults[5].Outputs["key"] != "BASE-1" || memoResults[5].Outputs["existed"] != "true" {
		t.Errorf("phase 01 finds the task the run made: %v", memoResults[5].Outputs)
	}
	if len(memo.attached) != 2 {
		t.Errorf("both tasks are born contracted: %v", memo.attached)
	}
}

func TestTheRunMemoForgetsAfterItsTTL(t *testing.T) {
	world := newPlaneWorld()
	m := newRunMemo()
	now := time.Now()
	m.now = func() time.Time { return now }
	plane := memoTaskPlane{taskPlane: world, memo: m}
	if _, err := world.CreateEpic(context.Background(), "ws_1", remotestate.EpicCreateRequest{Slug: "todos"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := plane.GetEpic(context.Background(), "ws_1", "todos"); err != nil {
			t.Fatal(err)
		}
	}
	if world.calls["GetEpic"] != 1 {
		t.Fatalf("a repeated read inside the TTL is the run's: %d reads", world.calls["GetEpic"])
	}
	now = now.Add(runMemoTTL)
	if _, err := plane.GetEpic(context.Background(), "ws_1", "todos"); err != nil {
		t.Fatal(err)
	}
	if world.calls["GetEpic"] != 2 {
		t.Errorf("past the TTL the platform is asked again: %d reads", world.calls["GetEpic"])
	}
}

// Three single-template hooks, as every baseline wrote them: the connections
// and the secrets are read once for the run, and each hook still creates its
// own key and keeps what exists.
func TestTheRunMemoReadsTheSecretsOnceForAPhasesHooks(t *testing.T) {
	f := cloudflareWorkspace()
	plane := memoSecretsPlane{secretsPlane: f, memo: newRunMemo()}
	for _, h := range []struct{ key, template string }{
		{"CLOUDFLARE_API_TOKEN", "workers-deploy"}, {"CLOUDFLARE_D1_TOKEN", "d1-edit"}, {"CLOUDFLARE_ACCOUNT_ID", "account-id"},
	} {
		in := reconcileInput(t, map[string]any{"provider": "cloudflare", "keys": []any{h.key}, "template": h.template})
		res, err := reconcileOn(context.Background(), plane, "ws_1", in)
		if err != nil || res.Outputs["created"] != h.key {
			t.Fatalf("%s: %v %v", h.key, res.Outputs, err)
		}
	}
	if f.listCalls != 1 || f.secretLists != 1 || len(f.created) != 3 {
		t.Errorf("one connections read, one secrets read, three creates: %d, %d, %d", f.listCalls, f.secretLists, len(f.created))
	}
	// A re-run of the first hook finds the key this run made, from the memo.
	in := reconcileInput(t, map[string]any{"provider": "cloudflare", "keys": []any{"CLOUDFLARE_API_TOKEN"}, "template": "workers-deploy"})
	res, err := reconcileOn(context.Background(), plane, "ws_1", in)
	if err != nil || res.Outputs["kept"] != "CLOUDFLARE_API_TOKEN" || len(f.created) != 3 {
		t.Errorf("a key this run made is kept: %v %v (%d creates)", res.Outputs, err, len(f.created))
	}
}

// A remembered "not connected" is never the reason to wait: the connections
// are read again first, and a provider connected since is used.
func TestTheRunMemoRereadsTheConnectionsBeforeAWait(t *testing.T) {
	f := &fakeConfig{connectsAt: 2}
	plane := memoSecretsPlane{secretsPlane: f, memo: newRunMemo()}
	if _, err := plane.ListConnections(context.Background(), "ws_1"); err != nil {
		t.Fatal(err)
	}
	in := reconcileInput(t, map[string]any{"provider": "cloudflare", "keys": []any{"K"}, "template": "workers-deploy"})
	res, err := reconcileOn(context.Background(), plane, "ws_1", in)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Pending != nil || res.Outputs["created"] != "K" {
		t.Errorf("the provider connected since the remembered read is used: %+v", res)
	}
	if f.listCalls != 2 {
		t.Errorf("one remembered read, one fresh read before deciding: %d", f.listCalls)
	}
}

var _ secretsPlane = memoSecretsPlane{}
var _ taskPlane = memoTaskPlane{}
