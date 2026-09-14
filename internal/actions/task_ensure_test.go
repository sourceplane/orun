package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// existingEpicErr builds the slug-taken 409 the real plane returns, so the
// adopt path is exercised through ExistingEpicOf rather than around it.
func existingEpicErr(existing *remotestate.PublicEpic) error {
	details, _ := json.Marshal(map[string]any{"existing": existing})
	return &remotestate.APIError{Status: http.StatusConflict, Details: details}
}

// fakePlane records what was asked of the task plane and answers from a small
// in-memory world.
type fakePlane struct {
	epics      map[string]*remotestate.EpicView
	tasks      map[string][]remotestate.PublicTask
	createdEp  int
	createdMs  int
	createdTsk int
	// existingSlug makes CreateEpic answer the way the real plane does when a
	// slug is taken: with the existing epic, not a conflict.
	existingSlug string
	lastTask     remotestate.TaskCreateRequest
	// attached records every contract attach, keyed by task key, so a test can
	// assert that a landing's task was born able to fold.
	attached  map[string]string
	attachErr error
}

func (f *fakePlane) CreateEpic(_ context.Context, _ string, req remotestate.EpicCreateRequest) (*remotestate.PublicEpic, error) {
	if req.Slug != "" && req.Slug == f.existingSlug {
		return nil, existingEpicErr(&remotestate.PublicEpic{ID: "epc_existing", Slug: req.Slug, Key: "EP-1"})
	}
	f.createdEp++
	return &remotestate.PublicEpic{ID: "epc_new", Slug: req.Slug, Key: "EP-9", Name: req.Name}, nil
}

func (f *fakePlane) GetEpic(_ context.Context, _, ref string) (*remotestate.EpicView, error) {
	v, ok := f.epics[ref]
	if !ok {
		return nil, fmt.Errorf("no epic %q", ref)
	}
	return v, nil
}

func (f *fakePlane) CreateMilestone(_ context.Context, _, epicRef string, req remotestate.MilestoneCreateRequest) (*remotestate.PublicMilestone, error) {
	f.createdMs++
	m := remotestate.PublicMilestone{ID: "mls_new", Name: req.Name, ExitCriteria: req.ExitCriteria}
	if v, ok := f.epics[epicRef]; ok {
		v.Milestones = append(v.Milestones, m)
	}
	return &m, nil
}

func (f *fakePlane) ListTasksWhere(_ context.Context, _ string, filter remotestate.TaskListFilter) (*remotestate.TasksList, error) {
	return &remotestate.TasksList{Tasks: f.tasks[filter.Epic]}, nil
}

func (f *fakePlane) AttachTaskContract(_ context.Context, _, keyOrID string, _ json.RawMessage, hash string) (*remotestate.TaskContractSeal, error) {
	if f.attachErr != nil {
		return nil, f.attachErr
	}
	if f.attached == nil {
		f.attached = map[string]string{}
	}
	f.attached[keyOrID] = hash
	return &remotestate.TaskContractSeal{}, nil
}

func (f *fakePlane) CreateTask(_ context.Context, _ string, req remotestate.TaskCreateRequest) (*remotestate.PublicTask, error) {
	f.createdTsk++
	f.lastTask = req
	return &remotestate.PublicTask{ID: "tsk_new", Key: "ORUN-7", TitleMirror: req.TitleMirror}, nil
}

func ensure(t *testing.T, f *fakePlane, params map[string]any) (Result, error) {
	t.Helper()
	resolved, err := Resolve("orun.task/ensure@v1", params)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return ensureOn(context.Background(), f, "ws_1", Input{Params: resolved})
}

func TestEnsureEpicCreatesWhenAbsent(t *testing.T) {
	f := &fakePlane{}
	res, err := ensure(t, f, map[string]any{"kind": "epic", "name": "Infra baselining", "slug": "infra-baselining"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Outputs["existed"] != "false" || res.Outputs["id"] != "epc_new" {
		t.Errorf("expected a fresh epic, got %v", res.Outputs)
	}
}

// A taken slug is an answer, not an error: that is what makes re-running a
// phase safe.
func TestEnsureEpicAdoptsAnExistingSlug(t *testing.T) {
	f := &fakePlane{existingSlug: "infra-baselining"}
	res, err := ensure(t, f, map[string]any{"kind": "epic", "name": "Infra baselining", "slug": "infra-baselining"})
	if err != nil {
		t.Fatalf("a taken slug must not be an error: %v", err)
	}
	if res.Outputs["existed"] != "true" || res.Outputs["id"] != "epc_existing" {
		t.Errorf("expected the existing epic, got %v", res.Outputs)
	}
	if f.createdEp != 0 {
		t.Error("nothing should have been created")
	}
}

func TestEnsureMilestoneIsIdempotentByNameWithinItsEpic(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{
		"infra-baselining": {Milestones: []remotestate.PublicMilestone{{ID: "mls_1", Name: "03-infrastructure"}}},
	}}
	res, err := ensure(t, f, map[string]any{"kind": "milestone", "name": "03-infrastructure", "epic": "infra-baselining"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Outputs["existed"] != "true" || res.Outputs["id"] != "mls_1" {
		t.Errorf("the existing milestone should be found, got %v", res.Outputs)
	}
	if f.createdMs != 0 {
		t.Error("re-running a phase must not make a second milestone")
	}
}

func TestEnsureMilestoneCreatesANewName(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{
		"infra-baselining": {Milestones: []remotestate.PublicMilestone{{ID: "mls_1", Name: "03-infrastructure"}}},
	}}
	res, err := ensure(t, f, map[string]any{"kind": "milestone", "name": "04-workers", "epic": "infra-baselining"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Outputs["existed"] != "false" {
		t.Errorf("a new name is a new milestone, got %v", res.Outputs)
	}
	if f.createdMs != 1 {
		t.Errorf("expected one creation, got %d", f.createdMs)
	}
}

func TestEnsureMilestoneNeedsItsEpic(t *testing.T) {
	f := &fakePlane{}
	if _, err := ensure(t, f, map[string]any{"kind": "milestone", "name": "04-workers"}); err == nil {
		t.Fatal("a milestone with no epic has no identity; that must be refused")
	}
}

func TestEnsureTaskIsIdempotentByTitleWithinItsEpic(t *testing.T) {
	f := &fakePlane{tasks: map[string][]remotestate.PublicTask{
		"infra-baselining": {{ID: "tsk_1", Key: "ORUN-3", TitleMirror: "phase(05-edge): api-edge"}},
	}}
	res, err := ensure(t, f, map[string]any{
		"kind": "task", "name": "phase(05-edge): api-edge", "epic": "infra-baselining",
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Outputs["existed"] != "true" || res.Outputs["key"] != "ORUN-3" {
		t.Errorf("the existing task should be found, got %v", res.Outputs)
	}
	if f.createdTsk != 0 {
		t.Error("re-running a landing must not mint a second task")
	}
}

func TestEnsureTaskClubsItUnderEpicAndMilestone(t *testing.T) {
	f := &fakePlane{}
	if _, err := ensure(t, f, map[string]any{
		"kind": "task", "name": "phase(05-edge): api-edge",
		"epic": "infra-baselining", "milestone": "05-edge", "brief": "the edge",
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Epic and milestone must ride the SAME create — the plane resolves them
	// before minting the key, so a bad ref is a 422 rather than a half-made task.
	if f.lastTask.Epic != "infra-baselining" || f.lastTask.Milestone != "05-edge" {
		t.Errorf("the task should be born clubbed, got %+v", f.lastTask)
	}
	if f.lastTask.Brief != "the edge" {
		t.Errorf("the brief should ride the create, got %q", f.lastTask.Brief)
	}
}

func TestEnsureRejectsAnUnknownKind(t *testing.T) {
	f := &fakePlane{}
	_, err := ensure(t, f, map[string]any{"kind": "sprint", "name": "x"})
	if err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	if !strings.Contains(err.Error(), "epic, milestone or task") {
		t.Errorf("the error should name the kinds; got %v", err)
	}
}

func TestRollupReportsProgressAndIsNeverAnError(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{
		"infra-baselining": {Rollup: &remotestate.EpicRollup{Total: 8, Done: 6, Blocked: 1}},
	}}
	resolved, err := Resolve("orun.task/rollup@v1", map[string]any{"epic": "infra-baselining"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	res, err := rollupOn(context.Background(), f, "ws_1", Input{Params: resolved})
	if err != nil {
		t.Fatalf("outstanding work is not an error: %v", err)
	}
	if res.Outputs["total"] != "8" || res.Outputs["done"] != "6" || res.Outputs["blocked"] != "1" {
		t.Errorf("rollup numbers wrong: %v", res.Outputs)
	}
	if res.Outputs["complete"] != "false" {
		t.Errorf("6 of 8 is not complete, got %v", res.Outputs["complete"])
	}
}

func TestRollupIsCompleteWhenEveryTaskIsDone(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{
		"e": {Rollup: &remotestate.EpicRollup{Total: 3, Done: 3}},
	}}
	resolved, _ := Resolve("orun.task/rollup@v1", map[string]any{"epic": "e"})
	res, err := rollupOn(context.Background(), f, "ws_1", Input{Params: resolved})
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if res.Outputs["complete"] != "true" {
		t.Errorf("3 of 3 is complete, got %v", res.Outputs)
	}
}

// An epic with no tasks is not "complete" — nothing has been done.
func TestRollupOnAnEmptyEpicIsNotComplete(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{"e": {Rollup: &remotestate.EpicRollup{}}}}
	resolved, _ := Resolve("orun.task/rollup@v1", map[string]any{"epic": "e"})
	res, _ := rollupOn(context.Background(), f, "ws_1", Input{Params: resolved})
	if res.Outputs["complete"] != "false" {
		t.Errorf("an empty epic must not report complete, got %v", res.Outputs)
	}
}

// The contract (orun-bootstrap-engine BE-O9).
//
// This action's own reason for existing says a bootstrap must create its tasks
// CONTRACTED: a task whose contract never declared gates parks at `in_review`
// after its merge, and a bootstrap creates its tasks seconds before their PRs.
// Until BE-O9 the action had no way to say it — every landing it made would
// have sat un-folded forever.

const contractDoc = `apiVersion: orun.io/v1
kind: TaskContract
spec:
  goal: The API edge is the single front door
  affects:
    - apps/api-edge
  doneWhen:
    - stage and prod answer /health
  gates: []
`

// writeContract puts a contract document in a fake BASELINE directory and
// returns that directory — never the product tree, which is the distinction
// the path resolution exists for.
func writeContract(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEnsureTaskAttachesTheDeclaredContract(t *testing.T) {
	base := writeContract(t, "tasks/05-edge.TaskContract.yaml", contractDoc)
	f := &fakePlane{epics: map[string]*remotestate.EpicView{}}

	res, err := ensureOn(context.Background(), f, "ws_1", Input{
		Dir:     t.TempDir(),
		BaseDir: base,
		Params: map[string]any{
			"kind": "task", "name": "phase(05-edge): api-edge",
			"epic": "infra-baselining", "contract": "tasks/05-edge.TaskContract.yaml",
		},
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	key := res.Outputs["key"]
	if f.attached[key] == "" {
		t.Fatalf("task %s was created without a contract — its landing would park at in_review", key)
	}
}

// The path is the BASELINE's. A contract lives beside the blueprint and is
// deliberately not copied into the product, so resolving it against the
// product tree would find nothing — silently, if the action shrugged.
func TestTheContractPathResolvesAgainstTheBaselineNotTheProduct(t *testing.T) {
	base := writeContract(t, "tasks/05-edge.TaskContract.yaml", contractDoc)
	product := t.TempDir() // deliberately empty: the product carries no contracts

	f := &fakePlane{epics: map[string]*remotestate.EpicView{}}
	if _, err := ensureOn(context.Background(), f, "ws_1", Input{
		Dir: product, BaseDir: base,
		Params: map[string]any{"kind": "task", "name": "t", "contract": "tasks/05-edge.TaskContract.yaml"},
	}); err != nil {
		t.Fatalf("resolving against the baseline should have found the document: %v", err)
	}

	// With the two swapped there is nothing to find, and that is an error
	// rather than a task quietly created uncontracted.
	f2 := &fakePlane{epics: map[string]*remotestate.EpicView{}}
	_, err := ensureOn(context.Background(), f2, "ws_1", Input{
		Dir: base, BaseDir: product,
		Params: map[string]any{"kind": "task", "name": "t", "contract": "tasks/05-edge.TaskContract.yaml"},
	})
	if err == nil {
		t.Fatal("a missing contract document must fail, not create an uncontracted task")
	}
	if f2.createdTsk != 0 {
		t.Errorf("a missing contract cost %d minted key(s)", f2.createdTsk)
	}
}

// A malformed document must not cost a minted key — the same discipline
// `orun task create` keeps, for the same reason: the key is the scarce thing.
func TestAMalformedContractCostsNoMintedKey(t *testing.T) {
	base := writeContract(t, "tasks/bad.TaskContract.yaml", "apiVersion: orun.io/v1\nkind: NotAContract\n")
	f := &fakePlane{epics: map[string]*remotestate.EpicView{}}

	_, err := ensureOn(context.Background(), f, "ws_1", Input{
		Dir: t.TempDir(), BaseDir: base,
		Params: map[string]any{"kind": "task", "name": "t", "contract": "tasks/bad.TaskContract.yaml"},
	})
	if err == nil {
		t.Fatal("expected a malformed contract to fail")
	}
	if f.createdTsk != 0 {
		t.Errorf("a malformed contract cost %d minted key(s)", f.createdTsk)
	}
}

// A re-run finds the task it made last time — and re-attaches. The run that
// created it may have predated its contract, or died between the create and
// the attach; attaching the same bytes is a no-op by content hash, so the
// re-run heals rather than leaving a parked landing behind.
func TestAFoundTaskHasItsContractReattached(t *testing.T) {
	base := writeContract(t, "tasks/05-edge.TaskContract.yaml", contractDoc)
	f := &fakePlane{
		epics: map[string]*remotestate.EpicView{},
		tasks: map[string][]remotestate.PublicTask{
			"infra-baselining": {{ID: "tsk_1", Key: "BASE-7", TitleMirror: "phase(05-edge): api-edge"}},
		},
	}
	res, err := ensureOn(context.Background(), f, "ws_1", Input{
		Dir: t.TempDir(), BaseDir: base,
		Params: map[string]any{
			"kind": "task", "name": "phase(05-edge): api-edge",
			"epic": "infra-baselining", "contract": "tasks/05-edge.TaskContract.yaml",
		},
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if res.Outputs["existed"] != "true" {
		t.Fatalf("expected the existing task to be found, got %v", res.Outputs)
	}
	if f.attached["BASE-7"] == "" {
		t.Error("a re-run left the found task uncontracted")
	}
	if f.createdTsk != 0 {
		t.Errorf("a found task was created again (%d)", f.createdTsk)
	}
}

// No `contract:` is still legal. An uncontracted task is an honest state —
// just not one a bootstrap wants — so the action does not invent a document.
func TestNoContractDeclaredAttachesNothing(t *testing.T) {
	f := &fakePlane{epics: map[string]*remotestate.EpicView{}}
	if _, err := ensureOn(context.Background(), f, "ws_1", Input{
		Dir:    t.TempDir(),
		Params: map[string]any{"kind": "task", "name": "t"},
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.attached) != 0 {
		t.Errorf("attached %d contract(s) when none was declared", len(f.attached))
	}
}
