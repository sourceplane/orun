package main

// orun work (orun-cloud saas-work-gitops WG2, design §2 and §5): the
// repository's declared work tree — epic.yaml per epic under `work.epics`,
// contracts under `work.tasks` — judged before a merge. `check` reports
// every problem in the tree, plus the facts a PR at hand needs (which
// milestone its task closes, whether the diff touches the epic's status
// file). `sync` (WG3) reconciles the tree into the platform.

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/provenance"
	"github.com/sourceplane/orun/internal/workfile"
)

func registerWorkCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "The declared work tree: epics, their docs and task contracts, validated before the merge",
		Long: `A repository names where its work lives in intent.yaml:

  work:
    epics: work/epics     # one directory per epic, with an epic.yaml
    tasks: work/tasks     # <KEY>.TaskContract.yaml
    sync: on-merge        # off | on-merge

'check' reads the tree and reports every problem — a malformed epic.yaml,
a listed task with no contract, a contract in a reserved prefix that no
milestone lists — before a pull request merges. 'sync' reconciles the
tree into the platform.`,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newWorkCheckCommand())
	cmd.AddCommand(newWorkSyncCommand())
	root.AddCommand(cmd)
}

func newWorkCheckCommand() *cobra.Command {
	var (
		base   string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the declared work tree (exit 1 on any problem)",
		Long: `Read every epic.yaml under work.epics and every contract under
work.tasks, and print each problem as one sentence naming the file. With --base, also resolve the facts for the
current branch's task — the milestone that lists it, whether this PR would
close that milestone, and whether the diff touches the epic's status file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			layout := loadIntentForCloudConfig().WorkLayout()
			out := cmd.OutOrStdout()
			if !layout.Declared() {
				if asJSON {
					return encodeJSON(cmd, map[string]any{"declared": false})
				}
				fmt.Fprintln(out, "intent.yaml declares no work section — nothing to check (see `orun work --help`)")
				return nil
			}
			tree, err := workfile.Load(taskDocRoot(), layout)
			if err != nil {
				return fmt.Errorf("orun work check: %w", err)
			}
			var epic *epicFact
			var placedIn string
			if base != "" {
				branch, err := gitOut(cmd.Context(), "rev-parse", "--abbrev-ref", "HEAD")
				if err != nil {
					return fmt.Errorf("orun work check: %w", err)
				}
				if key := provenance.TaskKeyOfBranch(branch); key != "" {
					if pl, ok := tree.Placement[key]; ok {
						placedIn = pl.Epic.Slug + " / " + pl.Epic.Milestones[pl.Milestone].Name
						epic = resolveEpicFact(cmd.Context(), tree, pl, key, base, cmd.ErrOrStderr())
					}
				}
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{
					"declared": true, "layout": layout, "epics": len(tree.Epics), "contracts": len(tree.Contracts),
					"prefixes": tree.Prefixes(), "problems": tree.Problems, "placedIn": placedIn, "epic": epic,
				})
			}
			fmt.Fprintf(out, "%d epic(s), %d contract(s) under %s and %s; reserved prefixes: %s\n",
				len(tree.Epics), len(tree.Contracts), layout.Epics, layout.Tasks, dash(strings.Join(tree.Prefixes(), ", ")))
			for _, p := range tree.Problems {
				fmt.Fprintf(out, "error work-manifest   %s\n", p)
			}
			if placedIn != "" {
				fmt.Fprintf(out, "this branch's task sits in %s\n", placedIn)
				if epic != nil {
					fmt.Fprintf(out, "closes the milestone: %t; touches %s: %t\n", epic.ClosesMilestone, epic.StatusPath, epic.TouchesStatus)
				}
			}
			if len(tree.Problems) > 0 {
				return exitErr(1, "orun work check: %d problem(s) — fix the tree before opening the PR", len(tree.Problems))
			}
			fmt.Fprintln(out, "clean")
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base ref: also resolve the epic-status facts for this branch's task")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// epicFact is what `orun work check --base` reports about the milestone
// that lists the branch's task.
type epicFact struct {
	ClosesMilestone bool   `json:"closesMilestone"`
	TouchesStatus   bool   `json:"touchesStatus"`
	StatusPath      string `json:"statusPath,omitempty"`
}

// resolveEpicFact answers "does this PR close the milestone that lists key,
// and does the diff touch the epic's status file". Closing needs the other
// tasks' rungs from the platform; when the platform cannot be asked and
// the milestone lists more than this task, the fact is nil (nothing is
// said) rather than guessed.
func resolveEpicFact(ctx context.Context, tree *workfile.Tree, pl workfile.Placement, key, base string, errOut io.Writer) *epicFact {
	fact := &epicFact{StatusPath: pl.Epic.StatusPath()}
	changed, err := changedFilesSince(ctx, base)
	if err != nil {
		fmt.Fprintf(errOut, "orun work check: diff not read (%v)\n", err)
		return nil
	}
	for _, f := range changed {
		if f == fact.StatusPath {
			fact.TouchesStatus = true
		}
	}
	others := []string{}
	for _, k := range pl.Epic.Milestones[pl.Milestone].Tasks {
		if k != key {
			others = append(others, k)
		}
	}
	if len(others) == 0 {
		fact.ClosesMilestone = true
		return fact
	}
	client, err := cloudClient(ctx, "", "")
	if err != nil {
		fmt.Fprintf(errOut, "orun work check: epic-status not judged — the platform could not be asked for the milestone's other tasks (%v)\n", err)
		return nil
	}
	org := client.Scope().OrgID
	closes := true
	for _, k := range others {
		v, err := client.GetTaskVerdict(ctx, org, k)
		if err != nil {
			fmt.Fprintf(errOut, "orun work check: epic-status not judged — %s: %v\n", k, err)
			return nil
		}
		if v.Verdict.Rung != "done" {
			closes = false
			break
		}
	}
	fact.ClosesMilestone = closes
	return fact
}

// changedFilesSince lists the repo-relative files the branch changed
// against the base (origin/<base> when it exists, else <base>).
func changedFilesSince(ctx context.Context, base string) ([]string, error) {
	ref := "origin/" + base
	if _, err := gitOut(ctx, "rev-parse", "--verify", ref); err != nil {
		ref = base
		if _, err := gitOut(ctx, "rev-parse", "--verify", ref); err != nil {
			return nil, fmt.Errorf("base %q not found (nor origin/%s)", base, base)
		}
	}
	raw, err := gitOut(ctx, "diff", "--name-only", ref+"...HEAD")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if s := strings.TrimSpace(l); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
