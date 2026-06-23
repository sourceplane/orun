package statebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Run log read (NC3 tail — coordination-api.md §2). GET …/runs/{runId}/log
// returns the append-only event stream in wire form (nested payload); we decode
// it into the fold's flattened CoordinationEvent so the client can reduce it for
// `orun status` / `logs --follow`. The wire format is the contract; the
// flattened Go shape is a local convenience for Fold().

// wireEvent is the on-the-wire event envelope (payload nested, §8.1).
type wireEvent struct {
	Seq            int               `json:"seq"`
	Kind           string            `json:"kind"`
	RunID          string            `json:"runId"`
	JobID          string            `json:"jobId"`
	Actor          CoordinationActor `json:"actor"`
	At             string            `json:"at"`
	IdempotencyKey string            `json:"idempotencyKey"`
	V              int               `json:"v"`
	Payload        json.RawMessage   `json:"payload"`
}

func (w wireEvent) toEvent() CoordinationEvent {
	e := CoordinationEvent{
		Seq: w.Seq, Kind: w.Kind, RunID: w.RunID, JobID: w.JobID,
		Actor: w.Actor, At: w.At, IdempotencyKey: w.IdempotencyKey, V: w.V,
	}
	switch w.Kind {
	case EventRunCreated:
		var p struct {
			PlanDigest  string `json:"planDigest"`
			SourceHash  string `json:"sourceHash"`
			Environment string `json:"environment"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.PlanDigest, e.SourceHash = p.PlanDigest, p.SourceHash
	case EventJobClaimed:
		var p struct {
			RunnerID       string `json:"runnerId"`
			LeaseEpoch     int    `json:"leaseEpoch"`
			LeaseExpiresAt string `json:"leaseExpiresAt"`
			Attempt        int    `json:"attempt"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.RunnerID, e.LeaseEpoch, e.LeaseExpiresAt, e.Attempt = p.RunnerID, p.LeaseEpoch, p.LeaseExpiresAt, p.Attempt
	case EventLeaseRenewed:
		var p struct {
			RunnerID       string `json:"runnerId"`
			LeaseEpoch     int    `json:"leaseEpoch"`
			LeaseExpiresAt string `json:"leaseExpiresAt"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.RunnerID, e.LeaseEpoch, e.LeaseExpiresAt = p.RunnerID, p.LeaseEpoch, p.LeaseExpiresAt
	case EventLeaseExpired:
		var p struct {
			RunnerID   string `json:"runnerId"`
			LeaseEpoch int    `json:"leaseEpoch"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.RunnerID, e.LeaseEpoch = p.RunnerID, p.LeaseEpoch
	case EventJobSucceeded:
		var p struct {
			RunnerID     string `json:"runnerId"`
			LeaseEpoch   int    `json:"leaseEpoch"`
			ResultDigest string `json:"resultDigest"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.RunnerID, e.LeaseEpoch, e.ResultDigest = p.RunnerID, p.LeaseEpoch, p.ResultDigest
	case EventJobMemoized:
		var p struct {
			ResultDigest string `json:"resultDigest"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.ResultDigest = p.ResultDigest
	case EventJobFailed:
		var p struct {
			RunnerID   string `json:"runnerId"`
			LeaseEpoch int    `json:"leaseEpoch"`
			Reason     string `json:"reason"`
			ErrorText  string `json:"errorText"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.RunnerID, e.LeaseEpoch, e.Reason, e.ErrorText = p.RunnerID, p.LeaseEpoch, p.Reason, p.ErrorText
	case EventJobReady:
		var p struct {
			Attempt int `json:"attempt"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		e.Attempt = p.Attempt
	}
	return e
}

// ReadLog fetches the run's event stream from fromSeq (exclusive) onward and
// decodes it into fold-ready events. Fold(events, plan) yields authoritative
// run state on the client side — the same reduction the server uses. A positive
// waitSeconds turns the read into a long-poll: the server holds the request until
// an event lands past the cursor or the wait lapses (live-tail without busy poll).
func (c *CoordClient) ReadLog(ctx context.Context, runID string, fromSeq, waitSeconds int) ([]CoordinationEvent, error) {
	path := fmt.Sprintf("/runs/%s/log?from=%d", runID, fromSeq)
	if waitSeconds > 0 {
		path += fmt.Sprintf("&wait=%d", waitSeconds)
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read log %s: unexpected status %d", runID, resp.StatusCode)
	}
	var body struct {
		Events []wireEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]CoordinationEvent, len(body.Events))
	for i, w := range body.Events {
		out[i] = w.toEvent()
	}
	return out, nil
}
