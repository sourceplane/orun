package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objread"
)

// Packing weights for auto mode (specs/orun-runner-efficiency §5.4). A
// template can declare estimatedSeconds; `orun plan --weights` supplies
// measured ones instead: `history` reads them from the sealed executions in
// this repository's object model, a path reads a committed file (the
// lanes.lock the lane spec deferred), and `--write-weights` produces that
// file from history so the reviewed form can be checked in.

// weightsHistoryRuns is how many recent executions `--weights history` reads.
const weightsHistoryRuns = 10

// resolveWeights returns the per-job weights the flag names and where they
// came from ("history" or "file"), or nil for no flag.
func resolveWeights(ctx context.Context, spec string) (map[string]int, string, error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "":
		return nil, "", nil
	case "history":
		w, err := weightsFromHistory(ctx)
		return w, "history", err
	}
	data, err := os.ReadFile(spec)
	if err != nil {
		return nil, "", fmt.Errorf("--weights %s: %w", spec, err)
	}
	w, err := parseWeights(data)
	if err != nil {
		return nil, "", fmt.Errorf("--weights %s: %w", spec, err)
	}
	return w, "file", nil
}

// parseWeights reads a weights file: a JSON object of job id → seconds.
func parseWeights(data []byte) (map[string]int, error) {
	var raw map[string]float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("a weights file is a JSON object of job id to seconds: %w", err)
	}
	out := make(map[string]int, len(raw))
	for id, secs := range raw {
		if secs <= 0 {
			continue
		}
		n := int(secs + 0.5)
		if n < 1 {
			n = 1
		}
		out[id] = n
	}
	return out, nil
}

// weightsFromHistory reads the last few sealed executions and takes, per
// job, the median duration of its successful runs.
func weightsFromHistory(ctx context.Context) (map[string]int, error) {
	store, refs, root, err := openObjectModel()
	if err != nil {
		return nil, err
	}
	reader := objread.New(store, refs, root)
	views, err := reader.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("--weights history: %w", err)
	}
	samples := map[string][]float64{}
	read := 0
	for _, v := range views {
		if v.DryRun || v.Live {
			continue
		}
		full, err := reader.Get(ctx, v.ExecutionID)
		if err != nil {
			continue
		}
		for _, j := range full.Jobs {
			if j.Status != nodes.StatusSucceeded || j.StartedAt == nil || j.FinishedAt == nil {
				continue
			}
			if d := j.FinishedAt.Sub(*j.StartedAt).Seconds(); d > 0 {
				samples[j.JobID] = append(samples[j.JobID], d)
			}
		}
		read++
		if read >= weightsHistoryRuns {
			break
		}
	}
	return medianWeights(samples), nil
}

// medianWeights reduces per-job samples to one weight each: the median,
// rounded up to a whole second so a sub-second job still weighs one.
func medianWeights(samples map[string][]float64) map[string]int {
	out := make(map[string]int, len(samples))
	for id, s := range samples {
		if len(s) == 0 {
			continue
		}
		sort.Float64s(s)
		m := s[len(s)/2]
		if len(s)%2 == 0 {
			m = (s[len(s)/2-1] + s[len(s)/2]) / 2
		}
		n := int(m)
		if float64(n) < m {
			n++
		}
		if n < 1 {
			n = 1
		}
		out[id] = n
	}
	return out
}

// writeWeights writes a weights file, keys sorted, for review and commit.
func writeWeights(path string, weights map[string]int) error {
	ids := make([]string, 0, len(weights))
	for id := range weights {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("{\n")
	for i, id := range ids {
		sep := ","
		if i == len(ids)-1 {
			sep = ""
		}
		k, _ := json.Marshal(id)
		fmt.Fprintf(&b, "  %s: %d%s\n", k, weights[id], sep)
	}
	b.WriteString("}\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
