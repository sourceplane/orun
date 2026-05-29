package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cockpit/surface"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

func TestRunLogsPlainEmpty(t *testing.T) {
	var buf bytes.Buffer
	s := surface.Plain(&buf)
	v := viewmodel.LogsView{ExecID: "x", Run: viewmodel.RunView{PlanName: "p"}}
	out := strings.Join(RunLogs(s, v, LogsOptions{}), "\n")
	if !strings.Contains(out, "▲ orun p") || !strings.Contains(out, "Run: x") {
		t.Fatalf("missing header in %q", out)
	}
	if !strings.Contains(out, "No logs captured") {
		t.Fatalf("missing empty hint: %q", out)
	}
}

func TestRunLogsGroupsAndTruncates(t *testing.T) {
	var buf bytes.Buffer
	s := surface.Plain(&buf)
	v := viewmodel.LogsView{
		ExecID: "abc",
		Run:    viewmodel.RunView{PlanName: "release"},
		Entries: []viewmodel.LogEntry{
			{JobID: "api.stage.deploy", Component: "api", Environment: "stage",
				Short: "deploy", Status: "completed",
				Lines: []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10"},
				TotalLines: 10},
			{JobID: "api.stage.verify", Component: "api", Environment: "stage",
				Short: "verify", Status: "failed",
				Lines: []string{"boom: fatal"}, TotalLines: 1},
		},
	}
	out := strings.Join(RunLogs(s, v, LogsOptions{MaxLines: 3}), "\n")
	for _, want := range []string{"api", "stage", "deploy", "verify", "l1", "l2", "l3", "… 7 more lines", "boom: fatal"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "l4") {
		t.Errorf("did not truncate at MaxLines=3:\n%s", out)
	}
}
