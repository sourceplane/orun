package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/surface"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

func TestRunStatusPlainContainsBrand(t *testing.T) {
	var buf bytes.Buffer
	s := surface.Plain(&buf)
	v := viewmodel.RunView{
		ExecID:   "run-xyz",
		PlanID:   "a1b2c3",
		PlanName: "release",
		Status:   "running",
		Counts:   viewmodel.Counts{Total: 4, Completed: 1, Failed: 0, Running: 1, Pending: 2},
		Groups: []viewmodel.ComponentGroup{
			{Component: "api", Jobs: []viewmodel.Job{
				{Short: "deploy", Status: "running", Environment: "stage"},
				{Short: "verify", Status: "pending", Environment: "stage"},
			}},
		},
		Components: []string{"api"},
		MultiEnv:   false,
		StartedAt:  time.Now().Add(-30 * time.Second),
	}
	lines := RunStatus(s, v)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"▲ orun release", "Plan: a1b2c3", "Run: run-xyz",
		"1 component", "4 jobs", "api", "deploy", "verify"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestRunListEmpty(t *testing.T) {
	var buf bytes.Buffer
	s := surface.Plain(&buf)
	out := strings.Join(RunList(s, viewmodel.RunListView{}), "\n")
	if !strings.Contains(out, "No runs yet") {
		t.Fatalf("expected empty placeholder, got %q", out)
	}
}

func TestRunListShowsRows(t *testing.T) {
	var buf bytes.Buffer
	s := surface.Plain(&buf)
	list := viewmodel.RunListView{
		Runs: []viewmodel.RunSummary{
			{ExecID: "abc-1", PlanName: "p1", Status: "running",
				Counts: viewmodel.Counts{Total: 4, Completed: 1}, StartedAt: time.Now().Add(-time.Minute)},
			{ExecID: "abc-2", PlanName: "p2", Status: "failed",
				Counts: viewmodel.Counts{Total: 4, Completed: 2, Failed: 1}, StartedAt: time.Now().Add(-time.Hour)},
		},
	}
	out := strings.Join(RunList(s, list), "\n")
	for _, want := range []string{"abc-1", "abc-2", "running", "failed", "1 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond:                "500ms",
		2 * time.Second:                       "2.0s",
		3*time.Minute + 25*time.Second:        "3m25s",
		2*time.Hour + 5*time.Minute:           "2h05m",
	}
	for in, want := range cases {
		if got := shortDuration(in); got != want {
			t.Errorf("shortDuration(%v)=%q want %q", in, got, want)
		}
	}
}
