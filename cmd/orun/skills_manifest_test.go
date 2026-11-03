package main

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/provenance"
)

// SD4 (orun-cloud saas-skill-driven-agents design §9.1): the harness gets the
// PR manifest line — task, session and the loaded skills' pins — as
// ORUN_MANIFEST, rendered as `orun pr open` renders it, so a session that
// opens its pull request with gh writes the same block.
func TestSessionManifestEnv(t *testing.T) {
	line := sessionManifestEnv("APP-3", "as_1", []agent.SkillPin{{Name: "orunbase", Rev: "sha256:aa"}, {Name: "task-branch-pr", Rev: "sha256:bb"}})
	if !strings.HasPrefix(line, "ORUN_MANIFEST=<!-- orun:manifest\n") {
		t.Fatalf("line = %q", line)
	}
	m, err := provenance.ParseManifest(strings.TrimPrefix(line, "ORUN_MANIFEST="))
	if err != nil || m == nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	if m.Task != "APP-3" || m.Session != "as_1" || m.Version != provenance.ManifestVersion {
		t.Fatalf("manifest = %+v", m)
	}
	if len(m.Skills) != 2 || m.Skills[1].Name != "task-branch-pr" || m.Skills[1].Rev != "sha256:bb" {
		t.Fatalf("skills = %+v", m.Skills)
	}
	if got := sessionManifestEnv("", "as_1", nil); got != "" {
		t.Fatalf("no task, no manifest: %q", got)
	}
	// No skills loaded (the registry unreachable): still a manifest, no pins.
	if bare := sessionManifestEnv("APP-3", "", nil); !strings.Contains(bare, `"task": "APP-3"`) || strings.Contains(bare, "skills") {
		t.Fatalf("bare = %q", bare)
	}
}
