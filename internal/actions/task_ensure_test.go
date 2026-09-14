package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
