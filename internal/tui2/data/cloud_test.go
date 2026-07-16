package data

import (
	"context"
	"testing"
)

// TestParseCloudRunsShapes pins the defensive parse over both page shapes
// the platform contract has used: a bare array and a wrapped object.
func TestParseCloudRunsShapes(t *testing.T) {
	bare := []byte(`[
		{"id":"run_a1","status":"running","planName":"deploy checkout","createdAt":"2026-07-16T10:00:00Z"},
		{"runId":"run_b2","status":"succeeded","name":"plan payments","startedAt":"2026-07-16T09:00:00Z"},
		{"status":"orphan-no-id"}
	]`)
	v := parseCloudRuns(bare)
	if len(v.Runs) != 2 {
		t.Fatalf("bare: got %d rows", len(v.Runs))
	}
	if v.Runs[0].ExecID != "cloud:run_a1" || !IsCloudRun(v.Runs[0].ExecID) {
		t.Fatalf("provenance tag missing: %q", v.Runs[0].ExecID)
	}
	if v.Runs[1].PlanName != "plan payments" || v.Runs[1].StartedAt.IsZero() {
		t.Fatalf("field fallback broken: %+v", v.Runs[1])
	}

	wrapped := []byte(`{"runs":[{"id":"run_c3","status":"failed"}]}`)
	if v := parseCloudRuns(wrapped); len(v.Runs) != 1 || v.Runs[0].ExecID != "cloud:run_c3" {
		t.Fatalf("wrapped: %+v", v)
	}

	if v := parseCloudRuns([]byte(`{"unexpected":true}`)); len(v.Runs) != 0 {
		t.Fatalf("garbage must parse to empty, got %+v", v)
	}
}

// TestResolveCloudSignedOut: no env, no lane, no error — connection is a
// status.
func TestResolveCloudSignedOut(t *testing.T) {
	t.Setenv("ORUN_BACKEND_URL", "")
	t.Setenv("ORUN_WORKSPACE", "")
	t.Setenv("ORUN_ORG", "")
	if lane := ResolveCloud(context.Background(), "test"); lane != nil {
		t.Fatal("signed-out resolve must be nil")
	}
}

// TestWithCloudMergesAndDegrades: cloud rows append to local; a failing
// lane leaves local untouched (invariant §13.6).
func TestWithCloudMergesAndDegrades(t *testing.T) {
	local := SampleMock()
	merged := WithCloud(local, nil)
	if merged != Source(local) {
		t.Fatal("nil lane must return the local source unchanged")
	}
	// A real lane with a dead client: Runs errors, local rows survive.
	lane := &CloudLane{scope: "org_x/prj_y"}
	m := &mergedSource{Source: local, lane: lane}
	if m.Scope() != "org_x/prj_y" {
		t.Fatalf("scope = %q", m.Scope())
	}
	if !m.Capabilities().Remote {
		t.Fatal("merged source must report Remote")
	}
	v, err := m.Runs(context.Background())
	if err != nil {
		t.Fatalf("merged runs: %v", err)
	}
	want, _ := local.Runs(context.Background())
	if len(v.Runs) != len(want.Runs) {
		t.Fatalf("degraded lane must keep local rows: %d vs %d", len(v.Runs), len(want.Runs))
	}
}
