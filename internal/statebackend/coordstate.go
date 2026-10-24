package statebackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// The run's folded state in one read (coordination-api.md §2,
// GET …/runs/{runId}/state). A process that keeps a run's view (runView)
// used to assemble its first view from the whole event log and the plan
// object; the server holds the same fold, so the first read asks for it:
// O(jobs) bytes, one request, whatever the run's length
// (specs/orun-sync-efficiency P1, milestone SE2).

// RunStateRead is what the state read answers: the authoritative fold and,
// beside it, the wall-clock facts the presenter shows (absent on a fold the
// server built before it recorded them).
type RunStateRead struct {
	Fold    RunFoldState
	Timings *RunTimings
}

// RunTimings are the run's wall-clock facts as the server derives them from
// the events' own stamps: when the run first ran (its first claim), when it
// reached a terminal phase, and the same per job.
type RunTimings struct {
	StartedAt  string
	FinishedAt string
	Jobs       map[string]JobTimings
}

// JobTimings are a job's first start and its last finish; "" while unset.
type JobTimings struct {
	StartedAt  string
	FinishedAt string
}

// ErrStateReadUnsupported reports a server with no state read for the run:
// an older server without the route, or a run its coordination shard does
// not back. The caller reads the log instead.
var ErrStateReadUnsupported = errors.New("coordination state read unsupported")

// wireRunState is the server's RunFoldState (packages/contracts
// `state/coordination.ts`); its nullable fields decode to Go zero values.
type wireRunState struct {
	RunID      string `json:"runId"`
	PlanDigest string `json:"planDigest"`
	SourceHash string `json:"sourceHash"`
	Phase      string `json:"phase"`
	Jobs       map[string]struct {
		JobID          string `json:"jobId"`
		Phase          string `json:"phase"`
		Holder         string `json:"holder"`
		LeaseEpoch     int    `json:"leaseEpoch"`
		LeaseExpiresAt string `json:"leaseExpiresAt"`
		Attempt        int    `json:"attempt"`
		ResultDigest   string `json:"resultDigest"`
		ErrorText      string `json:"errorText"`
	} `json:"jobs"`
	Frontier []string `json:"frontier"`
	LastSeq  int      `json:"lastSeq"`
	Timings  *struct {
		StartedAt  string `json:"startedAt"`
		FinishedAt string `json:"finishedAt"`
		Jobs       map[string]struct {
			StartedAt  string `json:"startedAt"`
			FinishedAt string `json:"finishedAt"`
		} `json:"jobs"`
	} `json:"timings"`
}

func (w *wireRunState) toRead() *RunStateRead {
	out := &RunStateRead{Fold: RunFoldState{
		RunID:      w.RunID,
		PlanDigest: w.PlanDigest,
		SourceHash: w.SourceHash,
		Phase:      w.Phase,
		Jobs:       make(map[string]*JobFoldState, len(w.Jobs)),
		Frontier:   w.Frontier,
		LastSeq:    w.LastSeq,
	}}
	if out.Fold.Frontier == nil {
		out.Fold.Frontier = []string{}
	}
	for id, j := range w.Jobs {
		jobID := j.JobID
		if jobID == "" {
			jobID = id
		}
		out.Fold.Jobs[id] = &JobFoldState{
			JobID:          jobID,
			Phase:          j.Phase,
			Holder:         j.Holder,
			LeaseEpoch:     j.LeaseEpoch,
			LeaseExpiresAt: j.LeaseExpiresAt,
			Attempt:        j.Attempt,
			ResultDigest:   j.ResultDigest,
			ErrorText:      j.ErrorText,
		}
	}
	if w.Timings != nil {
		t := &RunTimings{StartedAt: w.Timings.StartedAt, FinishedAt: w.Timings.FinishedAt, Jobs: make(map[string]JobTimings, len(w.Timings.Jobs))}
		for id, jt := range w.Timings.Jobs {
			t.Jobs[id] = JobTimings{StartedAt: jt.StartedAt, FinishedAt: jt.FinishedAt}
		}
		out.Timings = t
	}
	return out
}

// ReadState fetches GET …/runs/{runId}/state: the fold the server holds for
// the run, with its timings. A 404 is ErrStateReadUnsupported, so a caller
// on an older server, or on a run the shard does not back, reads the log.
func (c *CoordClient) ReadState(ctx context.Context, runID string) (*RunStateRead, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/runs/%s/state", runID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return nil, ErrStateReadUnsupported
	default:
		return nil, fmt.Errorf("read state %s: unexpected status %d", runID, resp.StatusCode)
	}
	var w wireRunState
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		return nil, err
	}
	return w.toRead(), nil
}
