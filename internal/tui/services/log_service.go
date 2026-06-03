package services

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/runworktree"
)

// followPollInterval is how often follow-mode rescans the live working tree for
// newly written step logs. The runner streams each step's log file once,
// atomically, when the step completes (runworktree.SetStepLog), so polling for
// new/changed files — rather than byte-tailing a single file — matches how logs
// actually land on disk.
const followPollInterval = 250 * time.Millisecond

// TailLogs streams log lines for a job from the content-addressed object graph.
//
// It reads from two sources, branching on liveness:
//
//   - Live (an in-flight working tree exists): tail the working-tree step-log
//     files under run/<execID>/logs/<jobFolder>/. New steps appear as the run
//     progresses, so follow-mode rescans the live snapshot each tick.
//   - Sealed (no working tree): read each step's log content blob via
//     objread.StepLog and emit once — there are no files to tail.
//
// Two modes:
//
//   - Follow == false: read every existing step log for the job once, in
//     deterministic order, then close. Used for completed/historical runs.
//   - Follow == true: emit existing logs, then keep watching until the run
//     seals (or the context is cancelled). On seal the live files are gone, so
//     a final blob read catches anything that only landed in the object store.
//
// Remote-state log retrieval remains gated behind its own phase.
func (s *LiveOrunService) TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.RemoteState {
		return nil, errors.New("TailLogs: remote-state log retrieval not yet implemented")
	}
	if req.ExecID == "" {
		return nil, errors.New("TailLogs: ExecID is required")
	}
	if req.JobID == "" {
		return nil, errors.New("TailLogs: JobID is required")
	}
	if s.cfg.ObjectModelRoot == "" {
		return nil, errors.New("TailLogs: no object-model root configured")
	}
	root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")

	ch := make(chan LogEvent, 64)

	if !req.Follow {
		go func() {
			defer close(ch)
			emitted := map[string]bool{}
			if snap, live, _ := runworktree.LoadLive(root, req.ExecID); live {
				s.drainLiveLogs(ctx, ch, root, req, snap, emitted)
				return
			}
			s.drainSealedLogs(ctx, ch, req, emitted)
		}()
		return ch, nil
	}

	go func() {
		defer close(ch)
		// emitted tracks step ids already streamed so each is sent exactly once
		// across the rescans (and across the live→sealed handoff).
		emitted := map[string]bool{}
		ticker := time.NewTicker(followPollInterval)
		defer ticker.Stop()
		for {
			if snap, live, _ := runworktree.LoadLive(root, req.ExecID); live {
				s.drainLiveLogs(ctx, ch, root, req, snap, emitted)
			} else if s.sealedExecutionPresent(ctx, req.ExecID) {
				// Sealed and the working tree is gone: do a final blob read for
				// anything missed between the last tick and seal, then stop.
				s.drainSealedLogs(ctx, ch, req, emitted)
				return
			}
			// Not live and not yet sealed means the run hasn't flushed its first
			// state — keep waiting and let files appear.
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return ch, nil
}

// drainLiveLogs streams not-yet-emitted step logs for the job from the live
// working tree. Step ids come from the snapshot's latest attempt, and each log
// file is resolved through Snapshot.LogPath.
func (s *LiveOrunService) drainLiveLogs(ctx context.Context, ch chan<- LogEvent, root string, req LogRequest, snap *runworktree.Snapshot, emitted map[string]bool) {
	job := findSnapshotJob(snap, req.JobID)
	if job == nil {
		return
	}
	for _, st := range latestSnapshotSteps(job) {
		if req.StepID != "" && st.StepID != req.StepID {
			continue
		}
		if st.LogFile == "" || emitted[st.StepID] {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		path := snap.LogPath(root, job.Folder, st.StepID)
		if streamLogFile(ctx, ch, path, req.JobID, st.StepID) {
			emitted[st.StepID] = true
		}
	}
}

// drainSealedLogs reads not-yet-emitted step logs for the job from the sealed
// execution's content blobs.
func (s *LiveOrunService) drainSealedLogs(ctx context.Context, ch chan<- LogEvent, req LogRequest, emitted map[string]bool) {
	reader, ok := s.objReader()
	if !ok {
		return
	}
	view, err := reader.Get(ctx, req.ExecID)
	if err != nil {
		return
	}
	for _, j := range view.Jobs {
		if j.JobID != req.JobID && j.Folder != req.JobID {
			continue
		}
		for _, st := range latestViewSteps(j) {
			if req.StepID != "" && st.StepID != req.StepID {
				continue
			}
			if !st.HasLog || emitted[st.StepID] {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			content, lerr := reader.StepLog(ctx, view, j.JobID, st.StepID)
			if lerr != nil {
				continue
			}
			streamLogBytes(ctx, ch, content, req.JobID, st.StepID)
			emitted[st.StepID] = true
		}
	}
}

// sealedExecutionPresent reports whether execID resolves to a sealed execution
// (used by follow-mode to detect the live→sealed transition).
func (s *LiveOrunService) sealedExecutionPresent(ctx context.Context, execID string) bool {
	reader, ok := s.objReader()
	if !ok {
		return false
	}
	view, err := reader.Get(ctx, execID)
	return err == nil && !view.Live
}

// findSnapshotJob locates a job in a live snapshot by id (or folder).
func findSnapshotJob(snap *runworktree.Snapshot, jobID string) *runworktree.SnapshotJob {
	for i := range snap.Jobs {
		if snap.Jobs[i].JobID == jobID || snap.Jobs[i].Folder == jobID {
			return &snap.Jobs[i]
		}
	}
	return nil
}

// latestSnapshotSteps returns the steps of a job's most recent attempt.
func latestSnapshotSteps(job *runworktree.SnapshotJob) []runworktree.SnapshotStep {
	if n := len(job.Attempts); n > 0 {
		return job.Attempts[n-1].Steps
	}
	return nil
}

// latestViewSteps returns the steps of a sealed job's most recent attempt.
func latestViewSteps(j objread.JobView) []objread.StepView {
	if n := len(j.Attempts); n > 0 {
		return j.Attempts[n-1].Steps
	}
	return nil
}

// streamLogFile reads a single step log file and emits one LogEvent per line. It
// returns true when the file existed and was read (so follow-mode can mark it
// consumed) and false when the file is not yet present, in which case the caller
// should retry on a later poll.
func streamLogFile(ctx context.Context, ch chan<- LogEvent, path, jobID, stepID string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	now := time.Now()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return true
		case ch <- LogEvent{JobID: jobID, StepID: stepID, Line: scanner.Text(), Timestamp: now}:
		}
	}
	return true
}

// streamLogBytes emits one LogEvent per line from an in-memory log body (sealed
// content blob).
func streamLogBytes(ctx context.Context, ch chan<- LogEvent, body []byte, jobID, stepID string) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	now := time.Now()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case ch <- LogEvent{JobID: jobID, StepID: stepID, Line: scanner.Text(), Timestamp: now}:
		}
	}
}
