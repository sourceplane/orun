package trigger

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

func lanesIntent(t *testing.T, src string) *model.Intent {
	t.Helper()
	var intent model.Intent
	if err := yaml.Unmarshal([]byte(src), &intent); err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	return &intent
}

func TestValidateLanes_AbsentIsValid(t *testing.T) {
	intent := lanesIntent(t, `
components:
  - name: api
    type: helm
`)
	if intent.Execution.Lanes != nil {
		t.Fatalf("no execution.lanes must parse to nil, got %+v", intent.Execution.Lanes)
	}
	if errs := ValidateLanes(intent); len(errs) != 0 {
		t.Fatalf("an intent without lanes must be valid, got %v", errs)
	}
}

func TestValidateLanes_FullConfigParsesAndValidates(t *testing.T) {
	intent := lanesIntent(t, `
automation:
  triggerBindings:
    github-pull-request:
      on: { provider: github, event: pull_request }
execution:
  lanes:
    mode: auto
    maxLanes: 12
    concurrency: 6
    onCycle: reject
    groups:
      fleet: { concurrency: 8 }
    rules:
      - mode: job
        when: { triggerRef: github-pull-request }
components:
  - name: billing
    type: worker
    lane: fleet
  - name: console
    type: nextjs
    lane: isolated
`)
	l := intent.Execution.Lanes
	if l == nil || l.Mode != "auto" || l.MaxLanes != 12 || l.Concurrency != 6 || l.OnCycle != "reject" {
		t.Fatalf("lanes did not parse: %+v", l)
	}
	if l.Groups["fleet"].Concurrency != 8 || len(l.Rules) != 1 || l.Rules[0].When.TriggerRef != "github-pull-request" {
		t.Fatalf("groups/rules did not parse: %+v", l)
	}
	if intent.Components[0].Lane != "fleet" || intent.Components[1].Lane != "isolated" {
		t.Fatalf("component lanes did not parse: %q %q", intent.Components[0].Lane, intent.Components[1].Lane)
	}
	if errs := ValidateLanes(intent); len(errs) != 0 {
		t.Fatalf("valid configuration rejected: %v", errs)
	}
}

func TestValidateLanes_Rejections(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"unknown mode", "execution:\n  lanes:\n    mode: wave\n", `mode "wave" is not one of`},
		{"negative maxLanes", "execution:\n  lanes:\n    maxLanes: -1\n", "maxLanes must be at least 1"},
		{"negative concurrency", "execution:\n  lanes:\n    concurrency: -2\n", "concurrency must be at least 1"},
		{"unknown onCycle", "execution:\n  lanes:\n    onCycle: merge\n", `onCycle "merge" is not one of`},
		{"reserved group", "execution:\n  lanes:\n    groups:\n      isolated: {}\n", "is reserved"},
		{"malformed group", "execution:\n  lanes:\n    groups:\n      Fleet_A: {}\n", "lowercase letters"},
		{"group concurrency", "execution:\n  lanes:\n    groups:\n      fleet: { concurrency: -1 }\n", "groups.fleet.concurrency"},
		{"rule without mode", "execution:\n  lanes:\n    rules:\n      - when: { triggerRef: x }\n", "rules[0].mode is required"},
		{"rule bad mode", "automation:\n  triggerBindings:\n    x: {}\nexecution:\n  lanes:\n    rules:\n      - mode: wave\n        when: { triggerRef: x }\n", `rules[0].mode "wave"`},
		{"rule without trigger", "execution:\n  lanes:\n    rules:\n      - mode: job\n", "when.triggerRef is required"},
		{"rule unknown trigger", "execution:\n  lanes:\n    rules:\n      - mode: job\n        when: { triggerRef: nope }\n", `"nope" does not exist`},
		{"undeclared component lane", "components:\n  - name: api\n    lane: fleet\n", `lane "fleet" is not a group`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateLanes(lanesIntent(t, tc.src))
			if len(errs) == 0 {
				t.Fatalf("expected an error containing %q, got none", tc.want)
			}
			joined := FormatErrors(errs).Error()
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("expected %q in:\n%s", tc.want, joined)
			}
		})
	}
}

func TestComponentTreeCarriesLane(t *testing.T) {
	entry := model.FromComponent(model.Component{Name: "api", Lane: "fleet"}, "inline")
	if entry.Lane != "fleet" || entry.ToComponent().Lane != "fleet" {
		t.Fatalf("lane lost through the component cache: %+v", entry)
	}
}
