package platformmcp

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/workmcp"
)

var promptNames = []string{"investigate_failed_run", "access_review", "usage_review", "service_snapshot"}

// promptSampleArgs exercises every text branch of every prompt
// (investigate_failed_run renders differently with/without runId/project).
// Values deliberately avoid snake_case so the drift guard only sees
// tool-name-shaped tokens the prompt text itself introduces (the TS test's
// convention).
var promptSampleArgs = map[string][]map[string]string{
	"investigate_failed_run": {
		{"workspace": "acme"},
		{"workspace": "acme", "project": "prj1"},
		{"workspace": "acme", "runId": "01RUN"},
		{"workspace": "acme", "project": "prj1", "runId": "01RUN"},
	},
	"access_review":    {{"workspace": "acme"}},
	"usage_review":     {{"workspace": "acme"}},
	"service_snapshot": {{"workspace": "acme", "entityRef": "component:default/api"}},
}

func renderings(t *testing.T, name string) []string {
	t.Helper()
	p := &Provider{}
	var out []string
	for _, args := range promptSampleArgs[name] {
		text, owned := p.RenderPrompt(name, args)
		if !owned {
			t.Fatalf("%s not owned", name)
		}
		out = append(out, text)
	}
	return out
}

func TestPromptRoster(t *testing.T) {
	defs := (&Provider{}).Prompts()
	if len(defs) != len(promptNames) {
		t.Fatalf("prompts = %d, want %d", len(defs), len(promptNames))
	}
	for i, def := range defs {
		if def.Name != promptNames[i] {
			t.Errorf("prompt %d: name = %q, want %q", i, def.Name, promptNames[i])
		}
	}
	// An unknown prompt is not owned.
	if _, owned := (&Provider{}).RenderPrompt("no_such_prompt", nil); owned {
		t.Fatal("unknown prompt must not be owned")
	}
}

// TestPromptBranches pins the arg-conditional renderings of
// investigate_failed_run and the arg splicing of the other three.
func TestPromptBranches(t *testing.T) {
	texts := renderings(t, "investigate_failed_run")
	if !strings.Contains(texts[0], `runs_list with { workspace: "acme", status: "failed" }`) {
		t.Errorf("no-runId branch must orient via runs_list: %q", texts[0])
	}
	if !strings.Contains(texts[1], `runs_list with { workspace: "acme", status: "failed", project: "prj1" }`) {
		t.Errorf("project-only branch must scope runs_list: %q", texts[1])
	}
	if !strings.Contains(texts[2], "Investigate run 01RUN. If you don't know its project, find the run via runs_list first") {
		t.Errorf("runId-only branch: %q", texts[2])
	}
	if !strings.Contains(texts[3], "Investigate run 01RUN. Project: prj1.") {
		t.Errorf("runId+project branch: %q", texts[3])
	}
	for _, text := range texts {
		if !strings.Contains(text, "Diagnose a failed delivery run in workspace acme.") ||
			!strings.Contains(text, "runs_get") || !strings.Contains(text, "runs_read_logs") {
			t.Errorf("investigate workflow incomplete: %q", text)
		}
	}

	if text := renderings(t, "access_review")[0]; !strings.Contains(text, "Produce an access review for workspace acme.") ||
		!strings.Contains(text, "access_explain") || !strings.Contains(text, "security_events_list") || !strings.Contains(text, "audit_search") {
		t.Errorf("access_review: %q", text)
	}
	if text := renderings(t, "usage_review")[0]; !strings.Contains(text, "usage_summary") ||
		!strings.Contains(text, "quota_check") || !strings.Contains(text, "billing_summary") {
		t.Errorf("usage_review: %q", text)
	}
	if text := renderings(t, "service_snapshot")[0]; !strings.Contains(text, "Brief me on service component:default/api in workspace acme.") ||
		!strings.Contains(text, `catalog_get_entity ({ workspace, entityRef: "component:default/api" })`) ||
		!strings.Contains(text, "catalog_read_doc") || !strings.Contains(text, "runs_list") {
		t.Errorf("service_snapshot: %q", text)
	}
}

// TestPromptToolDriftGuard ports the TS plane's guard: every snake_case
// token in rendered prompt text must be a tool on the composed roster (work
// + platform + built-in — the 41 tools one serve advertises), so a tool
// rename breaks this test instead of silently orphaning a prompt.
func TestPromptToolDriftGuard(t *testing.T) {
	roster := map[string]bool{}
	for _, tool := range workmcp.Tools() {
		roster[tool.Name] = true
	}
	for _, tool := range (&Provider{}).Tools() {
		roster[tool.Name] = true
	}
	for _, tool := range (&mcpserve.ConnectionInfoProvider{}).Tools() {
		roster[tool.Name] = true
	}
	if len(roster) != 41 {
		t.Fatalf("composed roster = %d tools, want 41 (15 work + 25 platform + 1 built-in)", len(roster))
	}

	tokenPattern := regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)
	for _, name := range promptNames {
		distinct := map[string]bool{}
		for _, text := range renderings(t, name) {
			tokens := tokenPattern.FindAllString(text, -1)
			if len(tokens) == 0 {
				t.Errorf("%s references no tools", name)
			}
			for _, token := range tokens {
				if !roster[token] {
					t.Errorf("%s references unknown tool %q", name, token)
				}
				distinct[token] = true
			}
		}
		// A workflow, not a single call (the TS test's second guard).
		if len(distinct) < 2 {
			t.Errorf("%s names %d tools, want >= 2", name, len(distinct))
		}
	}
}
