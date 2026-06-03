package runner

import (
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
)

func TestSnapshotStateNilBeforeRun(t *testing.T) {
	r := &Runner{}
	if got := r.SnapshotState(); got != nil {
		t.Fatalf("SnapshotState before Run = %+v, want nil", got)
	}
}

func TestSnapshotStateDeepCopy(t *testing.T) {
	r := &Runner{}
	r.liveState = &execmodel.ExecState{
		ExecID:       "exec_1",
		PlanChecksum: "abc",
		Jobs: map[string]*execmodel.JobState{
			"a@deploy": {Status: "running", Steps: map[string]string{"build": "running"}},
			"b@deploy": nil,
		},
	}
	snap := r.SnapshotState()
	if snap == nil || snap.ExecID != "exec_1" || len(snap.Jobs) != 2 {
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if snap.Jobs["b@deploy"] != nil {
		t.Fatalf("nil job should stay nil")
	}
	// Mutating the snapshot must not affect the live state.
	snap.Jobs["a@deploy"].Status = "mutated"
	snap.Jobs["a@deploy"].Steps["build"] = "mutated"
	if r.liveState.Jobs["a@deploy"].Status != "running" || r.liveState.Jobs["a@deploy"].Steps["build"] != "running" {
		t.Fatalf("snapshot was not a deep copy")
	}
}
