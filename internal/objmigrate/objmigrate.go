// Package objmigrate ingests a legacy .orun/ state store (Phase 1/2 plans +
// executions) into the content-addressed object graph
// (specs/orun-object-model compatibility-and-migration.md). It is additive and
// idempotent — re-running produces the same objects and moves refs to the same
// targets — and never deletes anything from the legacy store.
//
// Like internal/objexec it is a transitional bridge that imports the legacy
// internal/state types; it is excluded from the object-model lint gate and is
// removed at the M12 cutover.
package objmigrate

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objexec"
	"github.com/sourceplane/orun/internal/state"
)

// Result summarizes a migration run.
type Result struct {
	Plans            int
	Executions       int
	OrphanExecutions int
	DryRun           bool
}

// Migrate ingests legacy into the object graph backed by store + refs. With
// dryRun it computes and reports without writing.
func Migrate(ctx context.Context, legacy *state.Store, store objectstore.ObjectStore, refs refstore.RefStore, dryRun bool) (Result, error) {
	res := Result{DryRun: dryRun}
	w := nodewriter.New(store, refs)
	algo := store.Algo()

	// Phase 1 — plans → revisions. Sorted for deterministic refs/revisions/latest.
	plans, err := legacy.ListPlans()
	if err != nil {
		return res, fmt.Errorf("objmigrate: list plans: %w", err)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Checksum < plans[j].Checksum })

	checksumToRev := map[string]objectstore.ObjectID{}
	for _, p := range plans {
		// SavePlan writes a checksum file plus latest/named aliases that share
		// the same checksum; ingest each distinct plan once.
		if _, seen := checksumToRev[p.Checksum]; seen {
			continue
		}
		planBytes, err := os.ReadFile(p.Path)
		if err != nil {
			continue // plan file vanished; skip
		}
		revID, err := ingestRevision(ctx, w, algo, p.Checksum, p.Name, p.Jobs, planBytes, dryRun)
		if err != nil {
			return res, err
		}
		checksumToRev[p.Checksum] = revID
		res.Plans++
	}

	// Phase 2 — executions → sealed ExecutionRuns under their revision.
	execs, err := legacy.ListExecutions()
	if err != nil {
		return res, fmt.Errorf("objmigrate: list executions: %w", err)
	}
	sort.Slice(execs, func(i, j int) bool { return execs[i].ID < execs[j].ID })

	for _, e := range execs {
		st, err := legacy.LoadState(e.ID)
		if err != nil || st == nil {
			continue // unreadable execution; skip
		}
		meta, _ := legacy.LoadMetadata(e.ID)

		revID, ok := checksumToRev[st.PlanChecksum]
		if !ok {
			// Orphan: the plan is gone. Synthesize a deterministic placeholder
			// revision so the execution is still ingested and reachable.
			revID, err = ingestRevision(ctx, w, algo, st.PlanChecksum, "migrated-unknown",
				len(st.Jobs), orphanPlanBytes(st.PlanChecksum), dryRun)
			if err != nil {
				return res, err
			}
			checksumToRev[st.PlanChecksum] = revID
			res.OrphanExecutions++
		}
		if !dryRun {
			if _, err := execseal.New(w).Seal(ctx, objexec.FromLegacyState(revID, "", e.ID, e.ID, st, meta)); err != nil {
				return res, fmt.Errorf("objmigrate: seal execution %s: %w", e.ID, err)
			}
		}
		res.Executions++
	}
	return res, nil
}

// ingestRevision computes (and optionally writes) the object-model revision for
// a legacy plan, publishing a deterministic by-hash ref so it stays reachable.
func ingestRevision(ctx context.Context, w *nodewriter.Writer, algo objectstore.Algo, checksum, name string, jobs int, planBytes []byte, dryRun bool) (objectstore.ObjectID, error) {
	rev := nodes.PlanRevision{
		Kind:           nodes.KindPlanRevision,
		HumanKey:       name,
		Scope:          nodes.RevisionScope{Mode: "full"},
		JobCount:       jobs,
		LegacyChecksum: checksum,
	}
	if dryRun {
		return nodes.RevisionID(algo, rev, planBytes)
	}
	id, _, err := w.WriteRevision(ctx, rev, planBytes,
		"revisions/by-hash/"+sanitizeSeg(checksum), "revisions/latest")
	if err != nil {
		return "", fmt.Errorf("objmigrate: write revision %s: %w", checksum, err)
	}
	return id, nil
}

func orphanPlanBytes(checksum string) []byte {
	return []byte(fmt.Sprintf(`{"migrated":"unknown","planChecksum":%q}`, checksum))
}

// sanitizeSeg folds a checksum into a single ref-path segment.
func sanitizeSeg(s string) string {
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
