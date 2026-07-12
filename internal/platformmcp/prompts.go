package platformmcp

// MCP prompts (orun-mcp UM6, mirroring the TS plane's prompts.ts): the four
// golden-path workflows. Names/titles/descriptions/args come from the
// manifest's reserved prompts section; the rendered texts are ported from
// prompts.ts verbatim — each names roster tools by exact name, and a drift
// guard in tests asserts every tool-name-shaped token in rendered text
// resolves on the composed roster.

import (
	"strings"

	"github.com/sourceplane/orun/internal/mcpserve"
)

// Prompts implements mcpserve.PromptProvider: the manifest's prompts,
// verbatim (never filtered under ReadOnly — prompts are text templates, and
// a read-only connection still resolves every referenced read tool).
func (p *Provider) Prompts() []mcpserve.PromptDef {
	out := make([]mcpserve.PromptDef, 0, len(embedded.Prompts))
	for _, mp := range embedded.Prompts {
		args := make([]mcpserve.PromptArg, 0, len(mp.Args))
		for _, a := range mp.Args {
			args = append(args, mcpserve.PromptArg{Name: a.Name, Description: a.Description, Required: a.Required})
		}
		out = append(out, mcpserve.PromptDef{Name: mp.Name, Title: mp.Title, Description: mp.Description, Arguments: args})
	}
	return out
}

// RenderPrompt implements mcpserve.PromptProvider: the prompt's single
// user-message text. Required arguments are enforced by the loop against the
// advertised defs before this runs.
func (p *Provider) RenderPrompt(name string, args map[string]string) (string, bool) {
	ws := args["workspace"]
	switch name {
	case "investigate_failed_run":
		var target string
		if runID := args["runId"]; runID != "" {
			target = "Investigate run " + runID + "."
			if project := args["project"]; project != "" {
				target += " Project: " + project + "."
			} else {
				target += " If you don't know its project, find the run via runs_list first — each run row carries its projectId."
			}
		} else {
			target = `Find the run: call runs_list with { workspace: "` + ws + `", status: "failed"`
			if project := args["project"]; project != "" {
				target += `, project: "` + project + `"`
			}
			target += ` } and take the newest failed run; its projectId is the project below.`
		}
		return strings.Join([]string{
			"Diagnose a failed delivery run in workspace " + ws + ".",
			"",
			"1. " + target,
			"2. Call runs_get ({ workspace, project, runId }) for the run and its plan-DAG job statuses.",
			"3. For every job whose status is failed or timed-out, call runs_read_logs ({ workspace, project, runId, jobId }); if the output is truncated or incomplete, page with fromSeq = the returned nextSeq.",
			"4. Report: the root cause (quote the decisive log lines), the failing job ids, the run/environment/commit identifiers, and one suggested next step. Cite exact ids so a human can verify.",
		}, "\n"), true
	case "access_review":
		return strings.Join([]string{
			"Produce an access review for workspace " + ws + ".",
			"",
			"1. Call access_explain ({ workspace }) — effective permissions with grant provenance (direct / team / account-cascade) plus the member and team rosters.",
			"2. Call security_events_list ({ workspace }) — recent security events.",
			"3. Call audit_search ({ workspace }) — a recent slice; focus on access-shaped actions (invites, role and team changes, key issuance).",
			"4. Output a markdown table: principal | access | via (grant provenance) | recent activity. Then flag follow-ups: elevated access with no recent activity, recent grant changes, and security events needing review. Cite ids and timestamps from tool output — never infer provenance.",
		}, "\n"), true
	case "usage_review":
		return strings.Join([]string{
			"Review usage, quota, and plan posture for workspace " + ws + ".",
			"",
			"1. Call usage_summary ({ workspace }) — current usage by dimension.",
			"2. Call quota_check ({ workspace }) — limits and remaining headroom.",
			"3. Call billing_summary ({ workspace }) — plan, entitlements, and invoices.",
			"4. Output a table: dimension | used | limit | % of quota. Flag any dimension at or above 80%, usage that looks anomalous against the plan's entitlements, and end with one recommendation (upgrade / cleanup / no action) citing the numbers.",
		}, "\n"), true
	case "service_snapshot":
		ref := args["entityRef"]
		return strings.Join([]string{
			"Brief me on service " + ref + " in workspace " + ws + ".",
			"",
			`1. Call catalog_get_entity ({ workspace, entityRef: "` + ref + `" }) — identity, owner, lifecycle, relations, provenance (note the sourceProjectId).`,
			"2. Call catalog_read_doc ({ workspace, entityRef }) to list its docs; read the overview by passing that row's digest back to catalog_read_doc.",
			"3. Call runs_list ({ workspace, project: <the entity's sourceProjectId>, limit: 10 }) — recent delivery history.",
			"4. Output a service brief: what it is (one paragraph), owner and lifecycle, dependencies and dependents from relations, doc highlights, and recent run health (statuses, plus the last failure if any). Cite entityRef, project, and run ids.",
		}, "\n"), true
	default:
		return "", false
	}
}
