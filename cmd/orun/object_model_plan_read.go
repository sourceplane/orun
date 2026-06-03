package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// object_model_plan_read.go resolves and lists plans from the content-addressed
// revision graph (each PlanRevision tree carries the compiled plan.json), so
// `orun get`/`describe`/`run` can read plans without the legacy plan store.
// Plans are published under refs/revisions/latest and revisions/by-hash/<sum>.

const revByHashPrefix = "revisions/by-hash/"

// openObjectStores opens the object + ref stores when the object model is active
// AND present on disk, returning the object-model root. ok=false ⇒ use the
// legacy store.
func openObjectStores() (store *objectstore.LocalStore, refs *refstore.LocalRefStore, root string, ok bool) {
	if !objectModelActive() {
		return nil, nil, "", false
	}
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
	// Prefix scan over by-hash refs.
	names, err := refs.List(ctx, revByHashPrefix)
	if err == nil {
		for _, name := range names {
			short := strings.TrimPrefix(name, revByHashPrefix)
			if strings.HasPrefix(short, sanitizeRevSeg(ref)) {
				if r, rerr := refs.Read(ctx, name); rerr == nil {
					return objectstore.ObjectID(r.Target), true
				}
			}
		}
	}
	return "", false
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
