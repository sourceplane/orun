package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/sourceplane/orun/internal/model"
)

// A lane-scoped step (`scope: lane` on a composition step) is one every job
// of a lane would run identically: `pnpm install`, a cache warm. Running it
// per job was the install the lane spec measured at ~40 s, done once per
// job instead of once per runner (specs/orun-runner-efficiency F3). laneSetup
// runs each distinct one once per process: the first job to reach it
// executes it in the shared source tree, before that job's private tree is
// staged; every later job with the same step waits for the result and reuses
// it. A step is the same when its command, shell, environment and working
// directory are the same after templating.
type laneSetup struct {
	mu   sync.Mutex
	runs map[string]*laneSetupRun
}

type laneSetupRun struct {
	owner  string
	done   chan struct{}
	output string
	err    error
}

func newLaneSetup() *laneSetup { return &laneSetup{runs: map[string]*laneSetupRun{}} }

// laneStepKey identifies a lane-scoped step across jobs.
func laneStepKey(step model.PlanStep, workingDir string, env map[string]string) string {
	b, _ := json.Marshal(struct {
		Run, Shell, Dir string
		Env             map[string]string
	}{step.Run, step.Shell, workingDir, env})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// claim returns the run for key and whether the caller owns it (and must run
// it, then settle it). A caller that does not own it waits on run.done.
func (l *laneSetup) claim(key, jobID string) (*laneSetupRun, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, ok := l.runs[key]; ok {
		return r, false
	}
	r := &laneSetupRun{owner: jobID, done: make(chan struct{})}
	l.runs[key] = r
	return r, true
}

// settle records the owner's result and releases the waiters.
func (r *laneSetupRun) settle(output string, err error) {
	r.output, r.err = output, err
	close(r.done)
}
