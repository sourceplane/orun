package model

import "testing"

func TestValidateExecForm(t *testing.T) {
	cases := []struct {
		name    string
		step    Step
		wantErr bool
	}{
		{"run only", Step{Name: "a", Run: "echo hi"}, false},
		{"use only", Step{Name: "a", Use: "some/action@v1"}, false},
		{"workflow only", Step{Name: "a", Workflow: "wf.yaml"}, false},
		{"none", Step{Name: "a"}, false},
		{"run+workflow", Step{Name: "a", Run: "x", Workflow: "wf.yaml"}, true},
		{"use+workflow", Step{Name: "a", Use: "x", Workflow: "wf.yaml"}, true},
		{"run+use", Step{Name: "a", Run: "x", Use: "y"}, true},
		{"all three", Step{Name: "a", Run: "x", Use: "y", Workflow: "z"}, true},
		{"whitespace-only workflow is not set", Step{Name: "a", Run: "x", Workflow: "   "}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.step.ValidateExecForm()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
