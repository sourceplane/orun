package logpipe

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/statebackend"
)

// The step coordinate survives the buffer and the spill file
// (orun-cloud saas-step-logs SL-R1).
//
// A spill file exists precisely because a run outlives a reachable backend, so
// it also outlives the binary that wrote it. Both directions of that matter and
// both are asserted here.

func code(n int) *int { return &n }

func TestStepSurvivesTheSpillFile(t *testing.T) {
	dir := t.TempDir()
	u := &recordingUploader{}
	p := New(u.up, Options{MaxMemBytes: 1, SpillPath: filepath.Join(dir, "spill.ndjson")})

	u.setFail(true)
	p.Append(context.Background(), "r", "j", "out\n", &statebackend.LogStep{
		StepID:   "deploy",
		Index:    4,
		Event:    statebackend.LogStepEnd,
		Status:   "failed",
		ExitCode: code(1),
	})
	u.setFail(false)

	if rep := p.Close(context.Background()); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	if len(u.steps) != 1 || u.steps[0] == nil {
		t.Fatalf("step lost across the spill: %+v", u.steps)
	}
	got := u.steps[0]
	if got.StepID != "deploy" || got.Index != 4 || got.Event != statebackend.LogStepEnd {
		t.Errorf("coordinate = %+v, want deploy/4/end", got)
	}
	if got.Status != "failed" || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Errorf("outcome = %q/%v, want failed/1", got.Status, got.ExitCode)
	}
}

// An exit code of 0 is a real outcome. If it round-tripped as absent, every
// clean step would arrive looking like a timeout.
func TestZeroExitCodeSurvivesTheSpillFile(t *testing.T) {
	dir := t.TempDir()
	u := &recordingUploader{}
	p := New(u.up, Options{MaxMemBytes: 1, SpillPath: filepath.Join(dir, "spill.ndjson")})

	u.setFail(true)
	p.Append(context.Background(), "r", "j", "out\n", &statebackend.LogStep{
		StepID: "checkout", Index: 0, Event: statebackend.LogStepEnd, Status: "succeeded", ExitCode: code(0),
	})
	u.setFail(false)
	_ = p.Close(context.Background())

	if len(u.steps) != 1 || u.steps[0] == nil || u.steps[0].ExitCode == nil {
		t.Fatalf("exit code lost: %+v", u.steps)
	}
	if *u.steps[0].ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", *u.steps[0].ExitCode)
	}
}

// The ndjson line is compatible in BOTH directions, which is what makes the
// spill format safe to extend: a file written before the coordinate existed
// decodes with no step (and uploads exactly as it always did), and a file
// written now is still readable by a decoder that has never heard of the key.
//
// This asserts the encoding directly rather than through a flush. The pipeline
// only drains a spill file it filled ITSELF — `flushLocked` gates on an
// in-memory `spilledCount` — so a file left behind by a previous process is a
// forensic record for the operator (Report names its path), not something a
// later run recovers. That is the package's existing design and not this
// change's to alter.
func TestSpillLineIsCompatibleBothWays(t *testing.T) {
	t.Run("a line from an older binary decodes with no step", func(t *testing.T) {
		var e entry
		if err := json.Unmarshal([]byte(`{"r":"r","j":"j","c":"old output\n"}`), &e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if e.Content != "old output\n" {
			t.Errorf("Content = %q", e.Content)
		}
		if e.Step != nil {
			t.Errorf("Step = %+v, want nil — an absent coordinate must stay absent", e.Step)
		}
	})

	t.Run("an unattributed line written now omits the key entirely", func(t *testing.T) {
		out, err := json.Marshal(entry{RunID: "r", JobID: "j", Content: "x\n"})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if strings.Contains(string(out), `"s"`) {
			t.Errorf("line = %s, want no step key so an older decoder sees what it always saw", out)
		}
	})

	t.Run("an attributed line round-trips", func(t *testing.T) {
		out, err := json.Marshal(entry{RunID: "r", JobID: "j", Content: "x\n", Step: &statebackend.LogStep{
			StepID: "deploy", Index: 4, Event: statebackend.LogStepEnd, Status: "failed", ExitCode: code(1),
		}})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		var back entry
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if back.Step == nil || back.Step.StepID != "deploy" || back.Step.Index != 4 {
			t.Fatalf("round-trip lost the coordinate: %+v", back.Step)
		}
		if back.Step.ExitCode == nil || *back.Step.ExitCode != 1 {
			t.Errorf("round-trip lost the exit code: %v", back.Step.ExitCode)
		}
	})
}
