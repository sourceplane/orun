package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// planHelperOutEnv, when set, turns TestPlanDeterminismHelper into a one-shot
// `orun plan -o $planHelperOutEnv` in the current directory.
const planHelperOutEnv = "ORUN_TEST_PLAN_HELPER_OUT"

// TestPlanDeterminismHelper is not a real test: TestPlanIsByteIdenticalAcrossRuns
// re-executes the test binary to run it, so every plan gets a fresh process
// (fresh CLI globals, fresh map hash seeds) exactly like repeated CLI calls.
func TestPlanDeterminismHelper(t *testing.T) {
	out := os.Getenv(planHelperOutEnv)
	if out == "" {
		t.Skip("helper process for TestPlanIsByteIdenticalAcrossRuns")
	}
	rootCmd.SetArgs([]string{"plan", "-o", out})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("orun plan: %v", err)
	}
}

// TestPlanIsByteIdenticalAcrossRuns plans the repo's examples/ fixture several
// times and asserts identical plan.json bytes. Go randomizes map iteration, so
// any map-order leak into job order, rendered steps or hashed metadata shows
// up as a differing checksum within a few runs.
func TestPlanIsByteIdenticalAcrossRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("plans the full examples/ fixture")
	}
	src, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatalf("copy examples fixture: %v", err)
	}

	const runs = 6
	var first []byte
	var firstPlan model.Plan
	for i := 0; i < runs; i++ {
		out := filepath.Join(t.TempDir(), "plan.json")
		cmd := exec.Command(os.Args[0], "-test.run=^TestPlanDeterminismHelper$", "-test.count=1")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), planHelperOutEnv+"="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %d: orun plan: %v\n%s", i, err, b)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("run %d: read plan: %v", i, err)
		}
		if i == 0 {
			first = got
			if err := json.Unmarshal(got, &firstPlan); err != nil {
				t.Fatalf("decode plan: %v", err)
			}
			continue
		}
		if !bytes.Equal(first, got) {
			var p model.Plan
			_ = json.Unmarshal(got, &p)
			t.Fatalf("run %d: plan.json differs from run 0 (checksum %s vs %s)",
				i, p.Metadata.Checksum, firstPlan.Metadata.Checksum)
		}
	}

	if len(firstPlan.Jobs) == 0 {
		t.Fatal("fixture produced no jobs; test is not exercising the planner")
	}
	if firstPlan.Metadata.GeneratedAt != "" {
		t.Errorf("metadata.generatedAt = %q; plans must not carry a wall-clock stamp", firstPlan.Metadata.GeneratedAt)
	}
}
