package actions

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/remotestate"
)

// A build's hooks ask the platform the same questions again and again.
//
// One build is one orun process, and its bookkeeping hooks each start from
// nothing: the epic's milestones are read once per milestone and once per task
// that clubs under one, the epic's tasks once per task, the connections and
// the secrets once per secrets hook. Every read is a round trip from the build
// box, and a round trip there costs seconds, so a bootstrap spent most of its
// first minute re-reading answers it already had.
//
// The run memo keeps those answers for the run. Writes go through to the
// platform exactly as before, and each successful write updates the
// remembered answer, so a later hook reads what the earlier one made. Answers
// are kept for runMemoTTL only, so a long-lived process never serves an old
// one, and an answer that would make a hook WAIT (no connection yet) is read
// again before the wait is declared.
//
// The memo never changes what a hook decides — only how many times it asks.

const runMemoTTL = 5 * time.Minute

type memoEntry[T any] struct {
	value T
	at    time.Time
}

type runMemo struct {
	mu        sync.Mutex
	now       func() time.Time
	epics     map[string]memoEntry[remotestate.EpicView]
	tasks     map[string]memoEntry[[]remotestate.PublicTask]
	contracts map[string]memoEntry[remotestate.TaskContractSeal]
	conns     map[string]memoEntry[[]configsurface.Connection]
	secrets   map[string]memoEntry[[]configsurface.SecretMeta]
}

func newRunMemo() *runMemo {
	return &runMemo{
		now:       time.Now,
		epics:     map[string]memoEntry[remotestate.EpicView]{},
		tasks:     map[string]memoEntry[[]remotestate.PublicTask]{},
		contracts: map[string]memoEntry[remotestate.TaskContractSeal]{},
		conns:     map[string]memoEntry[[]configsurface.Connection]{},
		secrets:   map[string]memoEntry[[]configsurface.SecretMeta]{},
	}
}

// buildMemo is the process's memo: one build, one process.
var buildMemo = newRunMemo()

func memoKey(parts ...string) string {
	b, _ := json.Marshal(parts)
	return string(b)
}

func fresh[T any](m *runMemo, e memoEntry[T], ok bool) bool {
	return ok && m.now().Sub(e.at) < runMemoTTL
}

// ── The task plane ──────────────────────────────────────────────────────

// memoTaskPlane answers a repeated epic read or task listing from the run,
// and does not send a contract this run already attached to the same task.
type memoTaskPlane struct {
	taskPlane
	memo *runMemo
}

func (p memoTaskPlane) GetEpic(ctx context.Context, org, ref string) (*remotestate.EpicView, error) {
	k := memoKey(org, ref)
	p.memo.mu.Lock()
	if e, ok := p.memo.epics[k]; fresh(p.memo, e, ok) {
		p.memo.mu.Unlock()
		view := e.value
		view.Milestones = append([]remotestate.PublicMilestone(nil), e.value.Milestones...)
		return &view, nil
	}
	p.memo.mu.Unlock()
	view, err := p.taskPlane.GetEpic(ctx, org, ref)
	if err != nil || view == nil {
		return view, err
	}
	p.memo.mu.Lock()
	p.memo.epics[k] = memoEntry[remotestate.EpicView]{value: copyEpicView(*view), at: p.memo.now()}
	p.memo.mu.Unlock()
	return view, nil
}

func (p memoTaskPlane) CreateMilestone(ctx context.Context, org, epicRef string, req remotestate.MilestoneCreateRequest) (*remotestate.PublicMilestone, error) {
	created, err := p.taskPlane.CreateMilestone(ctx, org, epicRef, req)
	if err != nil || created == nil {
		return created, err
	}
	k := memoKey(org, epicRef)
	p.memo.mu.Lock()
	if e, ok := p.memo.epics[k]; ok {
		e.value = copyEpicView(e.value)
		e.value.Milestones = append(e.value.Milestones, *created)
		p.memo.epics[k] = e
	}
	p.memo.mu.Unlock()
	return created, nil
}

func (p memoTaskPlane) ListTasksWhere(ctx context.Context, org string, filter remotestate.TaskListFilter) (*remotestate.TasksList, error) {
	// Only an epic's whole list is remembered: that is the find-by-title
	// read, and the only one a later create can keep current.
	if filter != (remotestate.TaskListFilter{Epic: filter.Epic}) || filter.Epic == "" {
		return p.taskPlane.ListTasksWhere(ctx, org, filter)
	}
	k := memoKey(org, filter.Epic)
	p.memo.mu.Lock()
	if e, ok := p.memo.tasks[k]; fresh(p.memo, e, ok) {
		p.memo.mu.Unlock()
		return &remotestate.TasksList{Tasks: append([]remotestate.PublicTask(nil), e.value...)}, nil
	}
	p.memo.mu.Unlock()
	list, err := p.taskPlane.ListTasksWhere(ctx, org, filter)
	if err != nil || list == nil {
		return list, err
	}
	p.memo.mu.Lock()
	p.memo.tasks[k] = memoEntry[[]remotestate.PublicTask]{value: append([]remotestate.PublicTask(nil), list.Tasks...), at: p.memo.now()}
	p.memo.mu.Unlock()
	return list, nil
}

func (p memoTaskPlane) CreateTask(ctx context.Context, org string, req remotestate.TaskCreateRequest) (*remotestate.PublicTask, error) {
	created, err := p.taskPlane.CreateTask(ctx, org, req)
	if err != nil || created == nil || req.Epic == "" {
		return created, err
	}
	k := memoKey(org, req.Epic)
	p.memo.mu.Lock()
	if e, ok := p.memo.tasks[k]; ok {
		task := *created
		if task.TitleMirror == "" {
			task.TitleMirror = req.TitleMirror
		}
		e.value = append(append([]remotestate.PublicTask(nil), e.value...), task)
		p.memo.tasks[k] = e
	}
	p.memo.mu.Unlock()
	return created, nil
}

// AttachTaskContract sends a contract once per task per run. Attaching the
// same bytes again is a no-op on the plane (it is keyed by content hash); the
// re-attach on a found task exists to heal an EARLIER run that failed between
// its create and its attach, and this run's own attach needs no healing.
func (p memoTaskPlane) AttachTaskContract(ctx context.Context, org, keyOrID string, wire json.RawMessage, hash string) (*remotestate.TaskContractSeal, error) {
	k := memoKey(org, keyOrID, hash)
	p.memo.mu.Lock()
	if e, ok := p.memo.contracts[k]; fresh(p.memo, e, ok) {
		p.memo.mu.Unlock()
		seal := e.value
		return &seal, nil
	}
	p.memo.mu.Unlock()
	seal, err := p.taskPlane.AttachTaskContract(ctx, org, keyOrID, wire, hash)
	if err != nil || seal == nil {
		return seal, err
	}
	p.memo.mu.Lock()
	p.memo.contracts[k] = memoEntry[remotestate.TaskContractSeal]{value: *seal, at: p.memo.now()}
	p.memo.mu.Unlock()
	return seal, nil
}

func copyEpicView(v remotestate.EpicView) remotestate.EpicView {
	v.Milestones = append([]remotestate.PublicMilestone(nil), v.Milestones...)
	return v
}

// ── The secrets plane ───────────────────────────────────────────────────

// memoSecretsPlane answers a repeated connections or secrets read from the
// run, and keeps the remembered secrets current as this run creates them.
type memoSecretsPlane struct {
	secretsPlane
	memo *runMemo
}

func (p memoSecretsPlane) ListConnections(ctx context.Context, org string) ([]configsurface.Connection, error) {
	p.memo.mu.Lock()
	if e, ok := p.memo.conns[org]; fresh(p.memo, e, ok) {
		p.memo.mu.Unlock()
		return append([]configsurface.Connection(nil), e.value...), nil
	}
	p.memo.mu.Unlock()
	return p.RefreshConnections(ctx, org)
}

// RefreshConnections reads the connections from the platform, never from the
// run: what a hook reads before it decides to WAIT for a connection.
func (p memoSecretsPlane) RefreshConnections(ctx context.Context, org string) ([]configsurface.Connection, error) {
	conns, err := p.secretsPlane.ListConnections(ctx, org)
	if err != nil {
		return conns, err
	}
	p.memo.mu.Lock()
	p.memo.conns[org] = memoEntry[[]configsurface.Connection]{value: append([]configsurface.Connection(nil), conns...), at: p.memo.now()}
	p.memo.mu.Unlock()
	return conns, nil
}

func secretsMemoKey(scope configsurface.Scope, chain bool) string {
	rung := "own"
	if chain {
		rung = "chain"
	}
	return memoKey(string(scope.Kind), scope.Org, scope.Project, scope.EnvID, rung)
}

func (p memoSecretsPlane) ListSecrets(ctx context.Context, scope configsurface.Scope, chain bool) ([]configsurface.SecretMeta, json.RawMessage, error) {
	k := secretsMemoKey(scope, chain)
	p.memo.mu.Lock()
	if e, ok := p.memo.secrets[k]; fresh(p.memo, e, ok) {
		p.memo.mu.Unlock()
		return append([]configsurface.SecretMeta(nil), e.value...), nil, nil
	}
	p.memo.mu.Unlock()
	metas, raw, err := p.secretsPlane.ListSecrets(ctx, scope, chain)
	if err != nil {
		return metas, raw, err
	}
	p.memo.mu.Lock()
	p.memo.secrets[k] = memoEntry[[]configsurface.SecretMeta]{value: append([]configsurface.SecretMeta(nil), metas...), at: p.memo.now()}
	p.memo.mu.Unlock()
	return metas, raw, nil
}

func (p memoSecretsPlane) CreateSecret(ctx context.Context, scope configsurface.Scope, req configsurface.CreateSecretRequest) (*configsurface.SecretMeta, error) {
	meta, err := p.secretsPlane.CreateSecret(ctx, scope, req)
	if err != nil {
		return meta, err
	}
	made := configsurface.SecretMeta{SecretKey: req.SecretKey}
	if meta != nil {
		made = *meta
		if made.SecretKey == "" {
			made.SecretKey = req.SecretKey
		}
	}
	p.memo.mu.Lock()
	for _, chain := range []bool{true, false} {
		k := secretsMemoKey(scope, chain)
		if e, ok := p.memo.secrets[k]; ok {
			e.value = append(append([]configsurface.SecretMeta(nil), e.value...), made)
			p.memo.secrets[k] = e
		}
	}
	p.memo.mu.Unlock()
	return meta, nil
}
