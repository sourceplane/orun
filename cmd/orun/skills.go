package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/remotestate"
)

// orun skills (orun-initiatives-v2 IS6, design §8) — the human/CI face of
// the hosted skill registry: list the merged default/org view, pull
// revisions as native skill files. Publishing stays with the console (and
// the cloud's PUT — work.approve); nothing here writes to the registry.

func registerSkillsCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Hosted agent playbooks: list the registry, pull native skill files",
		Long: `The skill registry (sealed, hosted policy): content-addressed revisions,
the Sourceplane defaults shadowed by anything your org publishes. Agents
consume these through skills_list/skill_get on the platform MCP and the
files 'orun agent run' materializes; this group is the same surface for
humans and CI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSkillsListCommand())
	cmd.AddCommand(newSkillsPullCommand())
	cmd.AddCommand(newSkillsStatusCommand())
	root.AddCommand(cmd)
}

func newSkillsListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Every skill's latest revision: name, rev, source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			skills, err := client.ListSkills(cmd.Context(), client.Scope().OrgID)
			if err != nil {
				return fmt.Errorf("orun skills list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, skills)
			}
			rows := make([][]string, 0, len(skills.Skills))
			for _, s := range skills.Skills {
				by := "-"
				if s.PublishedBy != nil {
					by = s.PublishedBy.ID
				}
				rows = append(rows, []string{s.Name, shortRevision(s.Rev), s.Source, by})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"NAME", "REV", "SOURCE", "PUBLISHED BY"}, rows))
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newSkillsPullCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		rev        string
		dir        string
		clientName string
		project    bool
	)
	cmd := &cobra.Command{
		Use:   "pull [name]",
		Short: "Materialize skills as native skill files (<dir>/<name>/SKILL.md, plus the bundle's files)",
		Long: `Pull one skill (or, with no name, the whole registry) and write each
revision as a native skill file the harness discovers on its own — SKILL.md
plus the bundle's references/ and templates/ beside it. The frontmatter
carries the pinned orun-rev — a skill on disk always names the revision it
is. A file a previous pull wrote and this revision no longer carries is
removed; anything else in the directory is left alone.

Where the files go: --dir names the directory outright; --client names the
coding-agent client and resolves its own skills directory (user-level, or
the repository's with --project):

  claude-code  ~/.claude/skills   (.claude/skills)
  codex        ~/.codex/skills    (.codex/skills)
  cursor       ~/.cursor/skills   (.cursor/skills)
  vscode       ~/.copilot/skills  (.github/skills)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if rev != "" && len(args) == 0 {
				return fmt.Errorf("orun skills pull: --rev needs a skill name")
			}
			if clientName != "" {
				if cmd.Flags().Changed("dir") {
					return fmt.Errorf("orun skills pull: --dir and --client name the same thing; pass one")
				}
				resolved, err := agent.ClientSkillsDir(clientName, project)
				if err != nil {
					return fmt.Errorf("orun skills pull: %w", err)
				}
				dir = resolved
			} else if project {
				return fmt.Errorf("orun skills pull: --project needs --client")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			var views []remotestate.SkillView
			if len(args) == 1 {
				view, err := client.GetSkill(cmd.Context(), org, args[0], rev)
				if err != nil {
					return fmt.Errorf("orun skills pull: %w", err)
				}
				views = append(views, *view)
			} else {
				views, err = fetchAllSkills(cmd.Context(), client, org)
				if err != nil {
					return fmt.Errorf("orun skills pull: %w", err)
				}
			}
			pins, err := agent.MaterializeSkills(dir, views)
			if err != nil {
				return fmt.Errorf("orun skills pull: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"dir": dir, "skills": pins})
			}
			files := make(map[string]int, len(views))
			for _, v := range views {
				files[v.Name] = len(v.Files)
			}
			for _, p := range pins {
				extra := ""
				if n := files[p.Name]; n > 0 {
					extra = fmt.Sprintf(" + %d file(s)", n)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%s)%s\n", filepath.Join(dir, p.Name, "SKILL.md"), shortRevision(p.Rev), extra)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "exact revision sha256:<hex> (single-name pulls)")
	cmd.Flags().StringVar(&dir, "dir", filepath.Join(".claude", "skills"), "target directory for skill files")
	cmd.Flags().StringVar(&clientName, "client", "", "the coding-agent client whose skills directory to write ("+strings.Join(agent.SkillsClients(), "|")+")")
	cmd.Flags().BoolVar(&project, "project", false, "with --client: the repository's skills directory rather than the user's")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// skillStatus is one row of `orun skills status`: how the local copy of a
// skill stands against the registry.
type skillStatus struct {
	Name   string `json:"name"`
	Local  string `json:"local,omitempty"`  // the orun-rev on disk
	Latest string `json:"latest,omitempty"` // the registry's latest
	Status string `json:"status"`           // current | stale | unknown | missing
}

func newSkillsStatusCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		dir        string
		clientName string
		project    bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Local skill copies against the registry: current, stale, unknown, missing",
		Long: `Compare the skills on disk — the orun-rev each SKILL.md carries — with the
registry's latest revisions. current: the same revision; stale: a real
revision since superseded (pull again); unknown: a revision the registry
never published (the file was edited, or came from somewhere else); missing:
a registry skill with no local copy. Exit code 1 when anything is not
current, so a session-start hook can call this and pull only when needed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clientName != "" {
				if cmd.Flags().Changed("dir") {
					return fmt.Errorf("orun skills status: --dir and --client name the same thing; pass one")
				}
				resolved, err := agent.ClientSkillsDir(clientName, project)
				if err != nil {
					return fmt.Errorf("orun skills status: %w", err)
				}
				dir = resolved
			}
			local, err := agent.ReadLocalPins(dir)
			if err != nil {
				return fmt.Errorf("orun skills status: %w", err)
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			list, err := client.ListSkills(cmd.Context(), org)
			if err != nil {
				return fmt.Errorf("orun skills status: %w", err)
			}
			latest := make(map[string]string, len(list.Skills))
			for _, s := range list.Skills {
				latest[s.Name] = s.Rev
			}
			rows := make([]skillStatus, 0, len(list.Skills)+len(local))
			seen := make(map[string]bool, len(local))
			notCurrent := 0
			for _, p := range local {
				seen[p.Name] = true
				row := skillStatus{Name: p.Name, Local: p.Rev, Latest: latest[p.Name]}
				switch {
				case p.Rev == row.Latest:
					row.Status = "current"
				default:
					// A pin the registry knows (superseded) versus one it never
					// published: a pinned read says which.
					if _, err := client.GetSkill(cmd.Context(), org, p.Name, p.Rev); err == nil {
						row.Status = "stale"
					} else {
						row.Status = "unknown"
					}
					notCurrent++
				}
				rows = append(rows, row)
			}
			for _, s := range list.Skills {
				if !seen[s.Name] {
					rows = append(rows, skillStatus{Name: s.Name, Latest: s.Rev, Status: "missing"})
					notCurrent++
				}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
			if asJSON {
				if err := encodeJSON(cmd, map[string]any{"dir": dir, "skills": rows, "current": notCurrent == 0}); err != nil {
					return err
				}
			} else {
				table := make([][]string, 0, len(rows))
				for _, r := range rows {
					table = append(table, []string{r.Name, dash(shortRevision(r.Local)), dash(shortRevision(r.Latest)), r.Status})
				}
				fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"NAME", "LOCAL", "LATEST", "STATUS"}, table))
			}
			if notCurrent > 0 {
				return fmt.Errorf("orun skills status: %d skill(s) not current in %s — `orun skills pull --dir %s` refreshes them", notCurrent, dir, dir)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", filepath.Join(".claude", "skills"), "directory holding the local skill files")
	cmd.Flags().StringVar(&clientName, "client", "", "the coding-agent client whose skills directory to read ("+strings.Join(agent.SkillsClients(), "|")+")")
	cmd.Flags().BoolVar(&project, "project", false, "with --client: the repository's skills directory rather than the user's")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// fetchAllSkills lists the registry then reads each latest body.
func fetchAllSkills(ctx context.Context, client *remotestate.Client, org string) ([]remotestate.SkillView, error) {
	list, err := client.ListSkills(ctx, org)
	if err != nil {
		return nil, err
	}
	views := make([]remotestate.SkillView, 0, len(list.Skills))
	for _, s := range list.Skills {
		view, err := client.GetSkill(ctx, org, s.Name, s.Rev)
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

// materializeHarnessSkills is the agent-run/serve hook (IS6): best-effort —
// fetch the registry, write native skill files into the harness workdir,
// and record the pins beside the MCP config for the PR manifest. A cloud
// miss (not linked, not logged in, offline) is a WARNING, never a failed
// session: the skills sharpen a run; their absence doesn't brick it.
func materializeHarnessSkills(ctx context.Context, backendURL, workspace, workdir string, errOut io.Writer) {
	client, err := cloudClient(ctx, backendURL, workspace)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	views, err := fetchAllSkills(ctx, client, client.Scope().OrgID)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	skillsDir := filepath.Join(workdir, ".claude", "skills")
	pins, err := agent.MaterializeSkills(skillsDir, views)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	if err := agent.WriteSkillPins(filepath.Join(".orun", "agent-mcp", "skills.json"), pins); err != nil {
		fmt.Fprintf(errOut, "orun agent: skill pins not recorded (%v)\n", err)
	}
	fmt.Fprintf(errOut, "orun agent: %d skill(s) materialized into %s (pins recorded for the manifest)\n", len(pins), skillsDir)
}

// shortRevision shortens a sha256:<hex> content address to its familiar
// 8-hex form. It moved here at the work-plane teardown (WT2) from the CLI
// group that used to define it; skills are content-addressed the same way.
func shortRevision(rev string) string {
	r := strings.TrimPrefix(rev, "sha256:")
	if len(r) > 8 {
		r = r[:8]
	}
	return r
}
