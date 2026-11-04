package model

import (
	"reflect"
	"strings"
	"testing"
)

// The discovery cache carries spec.preview both ways, like every authored
// field (see TestComponentTreeRoundTripPreservesSecretEnv).
func TestComponentTreeRoundTripPreservesPreview(t *testing.T) {
	pv := &ComponentPreview{Run: []string{"pnpm", "dev"}, Port: 4173, Needs: []string{"api"}, Path: "/app", Env: map[string]string{"A": "b"}}
	back := FromComponent(Component{Name: "web", Preview: pv}, "test").ToComponent()
	if !reflect.DeepEqual(back.Preview, pv) {
		t.Fatalf("round trip preview = %+v", back.Preview)
	}
}

func TestPreviewProblems(t *testing.T) {
	cases := []struct {
		run  []string
		port int
		path string
		want []string
	}{
		{[]string{"pnpm", "dev"}, 4173, "", nil},
		{[]string{"vite"}, 1024, "/", nil},
		{[]string{"vite"}, 65535, "/x", nil},
		{nil, 4173, "", []string{"run must be"}},
		{[]string{""}, 4173, "", []string{"run[0]"}},
		{[]string{"vite"}, 80, "", []string{"port must be"}},
		{[]string{"vite"}, 70000, "", []string{"port must be"}},
		{[]string{"vite"}, 0, "app", []string{"port must be", "path must start with /"}},
	}
	for _, c := range cases {
		got := PreviewProblems(c.run, c.port, c.path)
		if len(got) != len(c.want) {
			t.Errorf("PreviewProblems(%v, %d, %q) = %v, want %v", c.run, c.port, c.path, got, c.want)
			continue
		}
		for i := range got {
			if !strings.HasPrefix(got[i], c.want[i]) {
				t.Errorf("PreviewProblems(%v, %d, %q)[%d] = %q, want prefix %q", c.run, c.port, c.path, i, got[i], c.want[i])
			}
		}
	}
}
