package git

import (
	"testing"
)

func TestDiffIntent_NoChange(t *testing.T) {
	doc := []byte(`apiVersion: orun/v1
kind: Intent
metadata:
  name: test
discovery:
  roots: ["."]
components:
  - name: web
    type: app
`)
	result := DiffIntent(doc, doc)
	if result.Mode != IntentDiffNone {
		t.Fatalf("expected None, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_CommentsFormattingOnly(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
kind: Intent
metadata:
  name: test
discovery:
  roots: ["."]
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
kind: Intent
metadata:
  name: test
discovery:
  roots:
    - "."
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffNone {
		t.Fatalf("expected None for formatting-only change, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_TopLevelEnvChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
env:
  FOO: bar
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
env:
  FOO: baz
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for env change, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.ChangedSections) != 1 || result.ChangedSections[0] != "env" {
		t.Fatalf("expected ChangedSections=[env], got %v", result.ChangedSections)
	}
}

func TestDiffIntent_EnvironmentsChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
  staging:
    path: staging
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for environments change, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.ChangedSections) != 1 || result.ChangedSections[0] != "environments" {
		t.Fatalf("expected ChangedSections=[environments], got %v", result.ChangedSections)
	}
}

func TestDiffIntent_GroupsChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
groups:
  infra:
    path: infra
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
groups:
  infra:
    path: infra2
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for groups change, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.ChangedSections) != 1 || result.ChangedSections[0] != "groups" {
		t.Fatalf("expected ChangedSections=[groups], got %v", result.ChangedSections)
	}
}

func TestDiffIntent_MultipleSectionsChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
groups:
  infra:
    path: infra
env:
  FOO: bar
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
  staging:
    path: staging
groups:
  infra:
    path: infra2
env:
  FOO: baz
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.ChangedSections) != 3 {
		t.Fatalf("expected 3 changed sections, got %v", result.ChangedSections)
	}
	expected := []string{"env", "environments", "groups"}
	for i, s := range expected {
		if result.ChangedSections[i] != s {
			t.Fatalf("expected ChangedSections[%d]=%s, got %s", i, s, result.ChangedSections[i])
		}
	}
}

func TestDiffIntent_DiscoveryChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
discovery:
  roots: ["."]
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
discovery:
  roots: [".", "infra"]
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for discovery change, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_CompositionsChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
compositions:
  sources:
    - path: ./compositions
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
compositions:
  sources:
    - path: ./compositions
    - path: ./extra
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for compositions change, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_AutomationChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
automation:
  triggerBindings: []
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
automation:
  triggerBindings:
    - name: on-push
      event: push
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for automation change, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_ExecutionChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
execution:
  state:
    mode: local
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
execution:
  state:
    mode: remote
    backendUrl: https://example.com
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for execution change, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_InlineComponentAdded(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
  - name: api
    type: service
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffComponents {
		t.Fatalf("expected Components, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.Added) != 1 || result.Added[0] != "api" {
		t.Fatalf("expected Added=[api], got %v", result.Added)
	}
	if len(result.Modified) != 0 {
		t.Fatalf("expected no Modified, got %v", result.Modified)
	}
}

func TestDiffIntent_InlineComponentModified(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
    path: apps/web
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
    path: apps/web-v2
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffComponents {
		t.Fatalf("expected Components, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "web" {
		t.Fatalf("expected Modified=[web], got %v", result.Modified)
	}
}

func TestDiffIntent_InlineComponentRemoved(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
  - name: api
    type: service
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffComponents {
		t.Fatalf("expected Components, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "api" {
		t.Fatalf("expected Removed=[api], got %v", result.Removed)
	}
}

func TestDiffIntent_InlineComponentRenamed(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: old-api
    type: service
    path: apps/api
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: new-api
    type: service
    path: apps/api
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffComponents {
		t.Fatalf("expected Components, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.Added) != 1 || result.Added[0] != "new-api" {
		t.Fatalf("expected Added=[new-api], got %v", result.Added)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "old-api" {
		t.Fatalf("expected Removed=[old-api], got %v", result.Removed)
	}
}

func TestDiffIntent_ComponentsReorderedOnly(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
  - name: api
    type: service
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: api
    type: service
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffNone {
		t.Fatalf("expected None for reorder-only, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_BothComponentsAndEnvironmentsChanged(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
components:
  - name: web
    type: app
`)
	head := []byte(`apiVersion: orun/v1
environments:
  dev:
    path: dev
  staging:
    path: staging
components:
  - name: web
    type: app
  - name: api
    type: service
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global when both components and environments changed, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_InvalidBaseYAML(t *testing.T) {
	base := []byte(`{invalid yaml: [`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for parse failure, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_InvalidHeadYAML(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
`)
	head := []byte(`{invalid yaml: [`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffGlobal {
		t.Fatalf("expected Global for parse failure, got %s: %s", result.Mode, result.Reason)
	}
}

func TestDiffIntent_MultipleChanges(t *testing.T) {
	base := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
    path: apps/web
  - name: api
    type: service
    path: apps/api
  - name: db
    type: database
    path: infra/db
`)
	head := []byte(`apiVersion: orun/v1
components:
  - name: web
    type: app
    path: apps/web-v2
  - name: db
    type: database
    path: infra/db
  - name: worker
    type: service
    path: apps/worker
`)
	result := DiffIntent(base, head)
	if result.Mode != IntentDiffComponents {
		t.Fatalf("expected Components, got %s: %s", result.Mode, result.Reason)
	}
	if len(result.Added) != 1 || result.Added[0] != "worker" {
		t.Fatalf("expected Added=[worker], got %v", result.Added)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "web" {
		t.Fatalf("expected Modified=[web], got %v", result.Modified)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "api" {
		t.Fatalf("expected Removed=[api], got %v", result.Removed)
	}
}
