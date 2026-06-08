package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objread"
)

// object_model_plan_read.go resolves and lists plans from the content-addressed
// revision graph (each PlanRevision tree carries the compiled plan.json), so
// `orun get`/`describe`/`run` can read plans without the legacy plan store.
// Plans are published under refs/revisions/latest and revisions/by-hash/<sum>.

const revByHashPrefix = "revisions/by-hash/"

// openObjectStores opens the object + ref stores when the object graph is
// present on disk, returning the object-model root. ok=false ⇒ this workspace
// has no object graph yet.
func openObjectStores() (store *objectstore.LocalStore, refs *refstore.LocalRefStore, root string, ok bool) {
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, nil, "", false
	}
	root = objectModelRoot(abs)
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, nil, "", false
	}
	s, r, rt, err := openObjectModel()
	if err != nil {
		return nil, nil, "", false
	}
	return s, r, rt, true
}

// objResolveRevisionRef resolves a plan ref (latest/""/<hash>/<hash-prefix>) to a
// revision tree id.
func objResolveRevisionRef(store *objectstore.LocalStore, refs *refstore.LocalRefStore, ref string) (objectstore.ObjectID, bool) {
	ctx := context.Background()
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "latest" {
		if r, err := refs.Read(ctx, "revisions/latest"); err == nil {
			return objectstore.ObjectID(r.Target), true
		}
		return "", false
	}
	// Exact by-hash.
	if r, err := refs.Read(ctx, revByHashPrefix+sanitizeRevSeg(ref)); err == nil {
		return objectstore.ObjectID(r.Target), true
	}
	// Prefix scan over by-hash refs (a checksum prefix).
	names, err := refs.List(ctx, revByHashPrefix)
	if err != nil {
		return "", false
	}
	for _, name := range names {
		short := strings.TrimPrefix(name, revByHashPrefix)
		if strings.HasPrefix(short, sanitizeRevSeg(ref)) {
			if r, rerr := refs.Read(ctx, name); rerr == nil {
				return objectstore.ObjectID(r.Target), true
			}
		}
	}
	// Human-key match (e.g. `orun run rev-<key>`): resolve by the revision's
	// display key off the revision record.
	for _, name := range names {
		r, rerr := refs.Read(ctx, name)
		if rerr != nil {
			continue
		}
		revID := objectstore.ObjectID(r.Target)
		if rev, ok := objRevisionMeta(store, revID); ok && rev.HumanKey == ref {
			return revID, true
		}
	}
	return "", false
}

// objRevisionDetail is an object-model revision plus the newest trigger that
// produced it, for `orun describe revision|trigger`.
type objRevisionDetail struct {
	RevID      objectstore.ObjectID
	Revision   nodes.PlanRevision
	Trigger    nodes.TriggerOccurrence
	HasTrigger bool
	FromLatest bool // resolved via revisions/latest (ref "" or "latest")
}

// objResolveRevisionDetail resolves a revision ref (latest/""/<checksum>/<prefix>
// — the same grammar describePlan accepts) to its PlanRevision record and the
// newest trigger occurrence pointing at it. ok=false ⇒ no object graph or the
// ref did not resolve.
func objResolveRevisionDetail(ref string) (objRevisionDetail, bool) {
	store, refs, _, ok := openObjectStores()
	if !ok {
		return objRevisionDetail{}, false
	}
	revID, ok := objResolveRevisionRef(store, refs, ref)
	if !ok {
		return objRevisionDetail{}, false
	}
	rev, ok := objRevisionMeta(store, revID)
	if !ok {
		return objRevisionDetail{}, false
	}
	trimmed := strings.TrimSpace(ref)
	d := objRevisionDetail{
		RevID:      revID,
		Revision:   rev,
		FromLatest: trimmed == "" || trimmed == "latest",
	}
	if trg, tok := objTriggerForRevision(store, refs, revID); tok {
		d.Trigger = trg
		d.HasTrigger = true
	}
	return d, true
}

// objRevisionMeta reads revision.json (the PlanRevision record) from a revision
// tree.
func objRevisionMeta(store *objectstore.LocalStore, revID objectstore.ObjectID) (nodes.PlanRevision, bool) {
	ctx := context.Background()
	entries, err := store.GetTree(ctx, revID)
	if err != nil {
		return nodes.PlanRevision{}, false
	}
	for _, e := range entries {
		if e.Name != "revision.json" {
			continue
		}
		_, body, gerr := store.Get(ctx, e.ID)
		if gerr != nil {
			return nodes.PlanRevision{}, false
		}
		rev, derr := nodes.Decode[nodes.PlanRevision](body)
		if derr != nil {
			return nodes.PlanRevision{}, false
		}
		return rev, true
	}
	return nodes.PlanRevision{}, false
}

// objTriggerForRevision returns the newest trigger occurrence whose RevisionID
// points at revID, scanning the per-name triggers/<name>/latest refs. A revision
// reused across triggers (dedup) surfaces its most recent producer.
func objTriggerForRevision(store *objectstore.LocalStore, refs *refstore.LocalRefStore, revID objectstore.ObjectID) (nodes.TriggerOccurrence, bool) {
	ctx := context.Background()
	names, err := refs.List(ctx, "triggers/")
	if err != nil {
		return nodes.TriggerOccurrence{}, false
	}
	var best nodes.TriggerOccurrence
	found := false
	for _, name := range names {
		r, rerr := refs.Read(ctx, name)
		if rerr != nil {
			continue
		}
		_, body, gerr := store.Get(ctx, objectstore.ObjectID(r.Target))
		if gerr != nil {
			continue
		}
		trg, derr := nodes.Decode[nodes.TriggerOccurrence](body)
		if derr != nil {
			continue
		}
		if trg.RevisionID != string(revID) {
			continue
		}
		if !found || trg.CreatedAt.After(best.CreatedAt) {
			best = trg
			found = true
		}
	}
	return best, found
}

// objPlanFromRevision decodes the compiled plan from a revision tree.
func objPlanFromRevision(store *objectstore.LocalStore, revID objectstore.ObjectID) (*model.Plan, bool) {
	ctx := context.Background()
	entries, err := store.GetTree(ctx, revID)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.Name != "plan.json" {
			continue
		}
		_, body, gerr := store.Get(ctx, e.ID)
		if gerr != nil {
			return nil, false
		}
		var plan model.Plan
		if uerr := json.Unmarshal(body, &plan); uerr != nil {
			return nil, false
		}
		return &plan, true
	}
	return nil, false
}

// objResolvePlan resolves a plan ref to the compiled plan from the object model.
func objResolvePlan(ref string) (*model.Plan, bool) {
	store, refs, _, ok := openObjectStores()
	if !ok {
		return nil, false
	}
	revID, ok := objResolveRevisionRef(store, refs, ref)
	if !ok {
		return nil, false
	}
	return objPlanFromRevision(store, revID)
}

// objListPlanRows lists the revision-backed plans as legacy PlanEntry rows
// (newest-first), for `orun get plans`. ok=false ⇒ nothing in the object model.
func objListPlanRows() ([]execmodel.PlanEntry, bool) {
	store, refs, _, ok := openObjectStores()
	if !ok {
		return nil, false
	}
	ctx := context.Background()
	names, err := refs.List(ctx, revByHashPrefix)
	if err != nil || len(names) == 0 {
		return nil, false
	}
	rows := make([]execmodel.PlanEntry, 0, len(names))
	for _, name := range names {
		r, rerr := refs.Read(ctx, name)
		if rerr != nil {
			continue
		}
		checksum := strings.TrimPrefix(name, revByHashPrefix)
		row := execmodel.PlanEntry{
			Name:      checksum,
			Checksum:  checksum,
			CreatedAt: r.UpdatedAt,
		}
		if plan, pok := objPlanFromRevision(store, objectstore.ObjectID(r.Target)); pok {
			row.Jobs = len(plan.Jobs)
			if plan.Metadata.Name != "" {
				row.Name = plan.Metadata.Name
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows, true
}

// objListRevisionRows lists the object-model revisions as the rich `orun get
// plans` rows (revision key, trigger, plan hash, job count, latest execution),
// newest-first. ok=false ⇒ no object graph / no revisions. The trigger and the
// latest-execution columns are reconstructed from the graph (the producing
// TriggerOccurrence and an executions scan keyed by revision).
func objListRevisionRows() ([]revisionPlanRow, bool) {
	store, refs, root, ok := openObjectStores()
	if !ok {
		return nil, false
	}
	ctx := context.Background()
	names, err := refs.List(ctx, revByHashPrefix)
	if err != nil || len(names) == 0 {
		return nil, false
	}
	latestExec := objLatestExecByRevision(store, refs, root)

	type timedRow struct {
		row revisionPlanRow
		at  time.Time
	}
	timed := make([]timedRow, 0, len(names))
	for _, name := range names {
		r, rerr := refs.Read(ctx, name)
		if rerr != nil {
			continue
		}
		revID := objectstore.ObjectID(r.Target)
		rev, mok := objRevisionMeta(store, revID)
		if !mok {
			continue
		}
		row := revisionPlanRow{
			RevisionKey: rev.HumanKey,
			PlanHash:    shortHash(rev.PlanHash),
			JobCount:    rev.JobCount,
		}
		if row.RevisionKey == "" {
			row.RevisionKey = string(revID)
		}
		if trg, tok := objTriggerForRevision(store, refs, revID); tok {
			row.TriggerName = trg.TriggerName
		}
		if ev, eok := latestExec[string(revID)]; eok {
			row.LatestExec = ev.ExecutionKey
			row.LatestStatus = ev.Status
		}
		timed = append(timed, timedRow{row: row, at: r.UpdatedAt})
	}
	if len(timed) == 0 {
		return nil, false
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].at.After(timed[j].at) })
	rows := make([]revisionPlanRow, 0, len(timed))
	for _, x := range timed {
		rows = append(rows, x.row)
	}
	return rows, true
}

// objLatestExecByRevision indexes the newest sealed/live execution per revision
// id from a single executions scan, so `get plans` can show each revision's
// latest run without an O(revisions) walk.
func objLatestExecByRevision(store *objectstore.LocalStore, refs *refstore.LocalRefStore, root string) map[string]objread.ExecutionView {
	views, err := objread.New(store, refs, root).List(context.Background())
	if err != nil {
		return nil
	}
	out := make(map[string]objread.ExecutionView, len(views))
	for _, v := range views {
		if cur, ok := out[v.RevisionID]; !ok || v.StartedAt.After(cur.StartedAt) {
			out[v.RevisionID] = v
		}
	}
	return out
}

// sanitizeRevSeg folds a checksum/ref into the ref-path alphabet (matches the
// objplan writer's sanitizer).
func sanitizeRevSeg(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "x"
	}
	return out
}
