// Package workbrief rebuilds intent envelopes from the cloud fold summary
// and freezes them into sealed SpecSnapshots — shared by `orun spec pull`
// and the MCP's spec_get. Only intent crosses into a snapshot: contracts,
// docs, provenance — never a rung, assignee, or pin (the wire carries fold
// output; none of it enters the sealed bytes).
package workbrief

import (
	"fmt"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/worklens"
)

// SnapshotFromSummary filters one spec (and its tasks) out of the workspace
// summary and freezes it at the summary's log cursors.
func SnapshotFromSummary(workspace, slug string, summary *remotestate.WorkSummary) (*worklens.SpecSnapshot, error) {
	var specView *remotestate.WorkSpecView
	for i := range summary.Specs {
		if summary.Specs[i].Key == slug {
			specView = &summary.Specs[i]
			break
		}
	}
	if specView == nil {
		return nil, fmt.Errorf("workbrief: unknown spec %q in workspace", slug)
	}
	spec := worklens.Spec{
		APIVersion: worklens.APIVersion,
		Kind:       worklens.KindSpec,
		Key:        specView.Key,
		Workspace:  workspace,
		Title:      specView.Title,
		DocRef:     specView.DocRef,
		CreatedBy:  toLensActor(specView.CreatedBy),
		CreatedAt:  specView.CreatedAt,
	}
	var tasks []worklens.Task
	for _, t := range summary.Tasks {
		if t.Spec != slug {
			continue
		}
		tasks = append(tasks, worklens.Task{
			APIVersion: worklens.APIVersion,
			Kind:       worklens.KindTask,
			Key:        t.Key,
			Workspace:  workspace,
			Spec:       t.Spec,
			Title:      t.Title,
			Labels:     t.Labels,
			Contract:   ToLensContract(t.Contract),
			CreatedBy:  toLensActor(t.CreatedBy),
			CreatedAt:  t.CreatedAt,
		})
	}
	snap := worklens.NewSpecSnapshot(spec, tasks, "", summary.CoordSeq, summary.ObsSeq)
	return &snap, nil
}

func toLensActor(a remotestate.WorkActor) worklens.Actor {
	return worklens.Actor{Type: worklens.ActorType(a.Type), ID: a.ID, Via: a.Via}
}

// ToLensContract maps the wire contract onto the model contract.
func ToLensContract(c *remotestate.WorkContract) *worklens.Contract {
	if c == nil {
		return nil
	}
	return &worklens.Contract{
		Goal:         c.Goal,
		Affects:      c.Affects,
		DoneWhen:     c.DoneWhen,
		Gates:        c.Gates,
		DesignRefs:   c.DesignRefs,
		Deps:         c.Deps,
		GatesDefined: c.GatesDefined,
	}
}
