package model

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDependency_UnmarshalEdgeMode confirms the plan engine's parser of
// component.yaml accepts the per-edge mode shape the catalog parser accepts.
func TestDependency_UnmarshalEdgeMode(t *testing.T) {
	const in = `
dependsOn:
  - component: db-migrate
    mode: enforced
  - component: peer-worker
    mode: advisory
    modeRules:
      - mode: enforced
        when:
          triggerRef: github-tag-release
  - component: shared
`
	var c struct {
		DependsOn []Dependency `yaml:"dependsOn"`
	}
	if err := yaml.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(c.DependsOn) != 3 {
		t.Fatalf("dependsOn = %+v", c.DependsOn)
	}
	if c.DependsOn[0].Mode != DependencyModeEnforced {
		t.Errorf("dependsOn[0].mode = %q", c.DependsOn[0].Mode)
	}
	d := c.DependsOn[1]
	if d.Mode != DependencyModeAdvisory || len(d.ModeRules) != 1 ||
		d.ModeRules[0].Mode != DependencyModeEnforced || d.ModeRules[0].When.TriggerRef != "github-tag-release" {
		t.Errorf("dependsOn[1] = %+v", d)
	}
	if c.DependsOn[2].Mode != "" || c.DependsOn[2].ModeRules != nil {
		t.Errorf("dependsOn[2] should carry no mode, got %+v", c.DependsOn[2])
	}
}
