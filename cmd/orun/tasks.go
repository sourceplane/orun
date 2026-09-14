package main

// orun task (orun-tasks O2) — the CLI face of the task plane, split exactly
// along the epic's boundary: identity comes from the cloud (create/attach/
// show/list — the allocator is the single writer of keys, TK-I), while the
// contract is authored in the repo (tasks/<KEY>.TaskContract.yaml) and
// checked OFFLINE (check — the design §3.3 loop). The check verb is
// advisory by construction: plan-side evaluation guides, the workspace
// decides at enforcement (E1/E2), and effective access is always
// resolved_policy ∩ contract — narrower than either input (TK-7).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/taskfile"
	"github.com/sourceplane/orun/internal/taskobj"
)

func registerTaskCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Tasks: cloud-issued identity, repo-authored contracts, offline checks",
		Long: `A task binds work to an enforceable contract. The cloud allocator issues
every key (adopt > derive > mint — never invented client-side); the
contract lives in the repository as tasks/<KEY>.TaskContract.yaml and is
sealed by content hash wherever it travels. 'check' runs entirely offline.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTaskCreateCommand())
	cmd.AddCommand(newTaskAttachCommand())
	cmd.AddCommand(newTaskListCommand())
	cmd.AddCommand(newTaskShowCommand())
	cmd.AddCommand(newTaskCheckCommand())
	cmd.AddCommand(newTaskEpicCommand())
	cmd.AddCommand(newTaskMilestoneCommand())
	root.AddCommand(cmd)
}

// ── Containers (orun-baseline-tracking BT-O2) ────────────────────────────
//
// Epics and milestones nest under `orun task`: v2.54.0 retired the work
// plane's `orun epic` verb with "no replacement", and these are the task
// plane's containers — the programme tasks club under, and its phases —
// not that verb resurrected.

func newTaskEpicCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Epics: the container tasks and milestones club under",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTaskEpicCreateCommand())
	cmd.AddCommand(newTaskEpicShowCommand())
	cmd.AddCommand(newTaskEpicListCommand())
	return cmd
}

func newTaskEpicCreateCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		req        remotestate.EpicCreateRequest
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an orun-native epic; a taken slug is adopted, not suffixed",
		Long: `Create an epic. --slug is its human handle (every task-plane verb accepts
it); omit it and one is minted from the name. A slug already taken in the
workspace is NOT an error here: the existing epic is printed with
'reusing it' and the command exits 0 — an idempotent caller (a bootstrap
flow, a retrying agent) adopts it instead of minting a duplicate.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if req.Name == "" {
				return fmt.Errorf("orun task epic create: --name is required")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			epic, err := client.CreateEpic(cmd.Context(), org, req)
			existed := false
			if err != nil {
				existing := remotestate.ExistingEpicOf(err)
				if existing == nil {
					return fmt.Errorf("orun task epic create: %w", err)
				}
				epic, existed = existing, true
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"epic": epic, "existed": existed})
			}
			if existed {
				fmt.Fprintf(cmd.OutOrStdout(), "epic %s already exists (%s) — reusing it\n", epic.Slug, orDash(epic.Key))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created epic %s (%s, %s)\n", epic.Slug, orDash(epic.Key), epic.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&req.Name, "name", "", "display name (required)")
	cmd.Flags().StringVar(&req.Slug, "slug", "", "human handle, lowercase-hyphenated (default: minted from the name)")
	cmd.Flags().StringVar(&req.Description, "description", "", "what this is and what done looks like")
	cmd.Flags().StringVar(&req.TargetDate, "target-date", "", "YYYY-MM-DD")
	cmd.Flags().StringVar(&req.Owner, "owner", "", "owner: a subject ref (usr_… / sp_…) or 'me'")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskEpicShowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "show <epc_id|EP-n|slug>",
		Short: "One epic: its word beside the derived rollup, and its phases with their progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			view, err := client.GetEpic(cmd.Context(), client.Scope().OrgID, args[0])
			if err != nil {
				return fmt.Errorf("orun task epic show: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, view)
			}
			out := cmd.OutOrStdout()
			e := view.Epic
			fmt.Fprintf(out, "%s  %s  %s\n", e.Slug, orDash(e.Key), e.ID)
			if e.Name != "" {
				fmt.Fprintf(out, "name      %s\n", e.Name)
			}
			voice := "orun"
			if e.Provider != "" {
				voice = e.Provider
			}
			fmt.Fprintf(out, "state     %s: %s", voice, orDash(e.State))
			if view.Rollup != nil {
				fmt.Fprintf(out, " · orun: %d/%d done", view.Rollup.Done, view.Rollup.Total)
				if view.Rollup.Blocked > 0 {
					fmt.Fprintf(out, " · %d blocked", view.Rollup.Blocked)
				}
			}
			fmt.Fprintln(out)
			if e.Owner != "" {
				fmt.Fprintf(out, "owner     %s\n", e.Owner)
			}
			if e.TargetDate != "" {
				fmt.Fprintf(out, "target    %s\n", e.TargetDate)
			}
			if e.Health != "" {
				fmt.Fprintf(out, "health    %s%s\n", e.Health, map[bool]string{true: " — " + e.HealthNote, false: ""}[e.HealthNote != ""])
			}
			fmt.Fprintf(out, "tasks     %d\n", view.TaskCount)
			if len(view.Milestones) > 0 {
				progress := map[string]remotestate.EpicMilestoneRollup{}
				if view.Rollup != nil {
					for _, m := range view.Rollup.Milestones {
						progress[m.ID] = m
					}
				}
				rows := make([][]string, 0, len(view.Milestones))
				for _, m := range view.Milestones {
					p, ok := progress[m.ID]
					done := "-"
					if ok {
						done = fmt.Sprintf("%d/%d", p.Done, p.Total)
					}
					rows = append(rows, []string{m.ID, orDash(m.Name), done, orDash(m.TargetDate)})
				}
				fmt.Fprintln(out)
				fmt.Fprint(out, renderColumns([]string{"MILESTONE", "NAME", "DONE", "TARGET"}, rows))
			}
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskEpicListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "The workspace's epics: slug, key, id, state, provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			list, err := client.ListEpics(cmd.Context(), client.Scope().OrgID)
			if err != nil {
				return fmt.Errorf("orun task epic list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, list)
			}
			rows := make([][]string, 0, len(list.Epics))
			for _, e := range list.Epics {
				provider := e.Provider
				if provider == "" {
					provider = "orun"
				}
				rows = append(rows, []string{e.Slug, orDash(e.Key), e.ID, orDash(e.State), provider})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"SLUG", "KEY", "ID", "STATE", "PROVIDER"}, rows))
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskMilestoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Milestones: the phases of a native epic, in order, with exit criteria",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTaskMilestoneCreateCommand())
	return cmd
}

func newTaskMilestoneCreateCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		epic       string
		req        remotestate.MilestoneCreateRequest
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Add a phase to a native epic, positioned after a sibling (or first, or last)",
		Long: `Add a milestone — a phase — to an epic. Position is spoken: --after names
the sibling (mls_…) it follows, --first puts it first, neither puts it
last. Exit criteria are one per --exit-criteria flag. The same name twice
makes two phases: list the epic ('orun task epic show') first when you
mean "ensure". A tracker-mirrored epic refuses (its phases are the
tracker's).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if epic == "" {
				return fmt.Errorf("orun task milestone create: --epic is required (epc_…, EP-n, or the slug)")
			}
			if req.Name == "" {
				return fmt.Errorf("orun task milestone create: --name is required")
			}
			if req.After != "" && req.First {
				return fmt.Errorf("orun task milestone create: --after and --first are mutually exclusive")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			m, err := client.CreateMilestone(cmd.Context(), client.Scope().OrgID, epic, req)
			if err != nil {
				return fmt.Errorf("orun task milestone create: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"milestone": m})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created milestone %s (%s) in epic %s\n", orDash(m.Name), m.ID, epic)
			return nil
		},
	}
	cmd.Flags().StringVar(&epic, "epic", "", "the epic (epc_… id, EP-n key, or slug) (required)")
	cmd.Flags().StringVar(&req.Name, "name", "", "phase name (required)")
	cmd.Flags().StringVar(&req.TargetDate, "target-date", "", "YYYY-MM-DD")
	cmd.Flags().StringArrayVar(&req.ExitCriteria, "exit-criteria", nil, "one exit criterion (repeatable)")
	cmd.Flags().StringVar(&req.After, "after", "", "the sibling milestone (mls_…) this phase goes after")
	cmd.Flags().BoolVar(&req.First, "first", false, "put the phase first")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// taskDocRoot is where tasks/ documents are looked up: the repo root the
// intent file anchors (the same resolution the policy commands use).
func taskDocRoot() string {
	_, repoRoot := policyIntentContext()
	if repoRoot == "" {
		return "."
	}
	return repoRoot
}

func newTaskCreateCommand() *cobra.Command {
	var (
		workspace    string
		backendURL   string
		asJSON       bool
		adoptKey     string
		derive       string
		mintPrefix   string
		title        string
		brief        string
		epic         string
		milestone    string
		assignee     string
		contractPath string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Ask the allocator for a task; club it, brief it, and attach its contract in one breath",
		Long: `Create a task. The key comes from the cloud allocator's ladder — adopt a
tracker key (--adopt), derive from a repo issue (--derive web#123), or
mint from a sequence (--prefix). Where it belongs is set in the same
create (--epic, --milestone — resolved before the key is minted, so a bad
ref never leaves a half-made task), as are its brief (--brief) and who
takes it up (--assignee me).

The contract: --contract <file> attaches an explicit TaskContract document
(a template, unbound to any key — what a bootstrap keeps beside its
flows). Without it, tasks/<KEY>.TaskContract.yaml is attached if one
exists for the issued key. Either way the task is recorded in the local
object store (refs/tasks/<KEY>).

A task whose contract declares its gates — an explicit 'gates: []' means
merge alone finishes the work — can fold to done; one created without a
contract parks at in_review after its merge.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := remotestate.TaskCreateRequest{
				AdoptKey:    adoptKey,
				MintPrefix:  mintPrefix,
				TitleMirror: title,
				Brief:       brief,
				Epic:        epic,
				Milestone:   milestone,
				Assignee:    assignee,
			}
			if derive != "" {
				d, err := parseDeriveRef(derive)
				if err != nil {
					return err
				}
				req.Derive = d
			}
			// Read the template BEFORE creating: a malformed document must
			// not cost a minted key.
			var template *taskfile.Document
			if contractPath != "" {
				doc, err := taskfile.LoadTemplate(contractPath)
				if err != nil {
					return fmt.Errorf("orun task create: --contract: %w", err)
				}
				template = doc
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			task, err := client.CreateTask(cmd.Context(), org, req)
			if err != nil {
				return fmt.Errorf("orun task create: %w", err)
			}

			var (
				doc       *taskfile.Document
				hash      string
				attachErr error
			)
			if template != nil {
				doc, hash, attachErr = attachTemplate(cmd.Context(), client, org, task.Key, template)
			} else {
				doc, hash, attachErr = attachDocumentIfPresent(cmd.Context(), client, org, task.Key)
			}
			sealNote := sealTaskLocally(cmd.Context(), task, doc)

			if asJSON {
				return encodeJSON(cmd, map[string]any{
					"task":         task,
					"contractHash": hash,
				})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "created %s (%s)%s\n", task.Key, task.ID, taskWhere(task))
			switch {
			case attachErr != nil:
				fmt.Fprintf(out, "contract not attached: %v\n", attachErr)
			case doc != nil:
				fmt.Fprintf(out, "contract %s attached from %s%s\n", shortRevision(hash), doc.Path, gatesNote(doc))
			default:
				fmt.Fprintf(out, "no contract document (%s) — created without narrowing\n", taskfile.PathFor(taskDocRoot(), task.Key))
			}
			if sealNote != "" {
				fmt.Fprintln(out, sealNote)
			}
			if attachErr != nil {
				return fmt.Errorf("orun task create: contract attach: %w", attachErr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&adoptKey, "adopt", "", "tracker key to adopt (used only if free — the allocator decides)")
	cmd.Flags().StringVar(&derive, "derive", "", "derive the key from a repo issue (prefix#number, e.g. web#123)")
	cmd.Flags().StringVar(&mintPrefix, "prefix", "", "sequence prefix for minted keys (default TSK)")
	cmd.Flags().StringVar(&title, "title", "", "display title mirror")
	cmd.Flags().StringVar(&brief, "brief", "", "the brief: what done looks like, in a paragraph")
	cmd.Flags().StringVar(&epic, "epic", "", "club the task under this epic (epc_… id, EP-n key, or slug)")
	cmd.Flags().StringVar(&milestone, "milestone", "", "place the task in this milestone/phase (mls_… id; implies its epic)")
	cmd.Flags().StringVar(&assignee, "assignee", "", "who takes it up: a subject ref (usr_… / sp_…) or 'me'")
	cmd.Flags().StringVar(&contractPath, "contract", "", "attach this TaskContract document (a template; metadata.name optional) instead of tasks/<KEY>.TaskContract.yaml")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// taskWhere renders a task's membership for the create/show lines:
// " in milestone <name>" beats " under epic <slug>" (a milestone implies
// its epic); nothing when unclubbed.
func taskWhere(task *remotestate.PublicTask) string {
	switch {
	case task.Milestone != nil:
		name := task.Milestone.Name
		if name == "" {
			name = task.Milestone.ID
		}
		return " in milestone " + name
	case task.Epic != nil:
		return " under epic " + task.Epic.Slug
	}
	return ""
}

// gatesNote says what the attached contract lets a merge do — the one
// authored fact that decides whether the task can ever fold to done.
func gatesNote(doc *taskfile.Document) string {
	if doc == nil || doc.Contract == nil {
		return ""
	}
	switch {
	case len(doc.Contract.Gates) > 0:
		return fmt.Sprintf(" (%d gate(s))", len(doc.Contract.Gates))
	case doc.Contract.GatesDefined:
		return " (merge alone finishes it)"
	}
	return " (gates undeclared — a merge parks at in_review)"
}

func newTaskAttachCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "attach <key>",
		Short: "Seal tasks/<KEY>.TaskContract.yaml and upload it to the task",
		Long: `Attach (or revise) a task's contract from the repository document. The
contract is sealed locally (sha256 over canonical JSON), the server
recomputes the hash and refuses a mismatch, and the local object store's
task node moves to the new revision.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			doc, err := taskfile.FindForKey(taskDocRoot(), key)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			if doc == nil {
				return fmt.Errorf("orun task attach: no document at %s", taskfile.PathFor(taskDocRoot(), key))
			}
			hash, wire, err := contract.ContractID(doc.Contract)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			seal, err := client.AttachTaskContract(cmd.Context(), org, key, wire, hash)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			task, taskErr := client.GetTask(cmd.Context(), org, key)
			sealNote := ""
			if taskErr == nil {
				sealNote = sealTaskLocally(cmd.Context(), task, doc)
			}
			if asJSON {
				return encodeJSON(cmd, seal)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "contract %s attached to %s\n", shortRevision(seal.ContractHash), key)
			if sealNote != "" {
				fmt.Fprintln(cmd.OutOrStdout(), sealNote)
			}
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		filter     remotestate.TaskListFilter
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "The workspace's tasks: key, id, title mirror, where it belongs, contract",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			list, err := client.ListTasksWhere(cmd.Context(), client.Scope().OrgID, filter)
			if err != nil {
				return fmt.Errorf("orun task list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, list)
			}
			rows := make([][]string, 0, len(list.Tasks))
			for _, t := range list.Tasks {
				t := t
				rows = append(rows, []string{t.Key, t.ID, orDash(t.TitleMirror), orDash(strings.TrimSpace(taskWhere(&t))), orDash(shortRevision(t.ContractHash))})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"KEY", "ID", "TITLE", "WHERE", "CONTRACT"}, rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&filter.Epic, "epic", "", "only tasks clubbed under this epic (epc_… id or slug)")
	cmd.Flags().StringVar(&filter.Milestone, "milestone", "", "only tasks in this milestone (mls_… id)")
	cmd.Flags().StringVar(&filter.Assignee, "assignee", "", "only tasks assigned to this subject, 'me', or 'agents'")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskShowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "show <key|tsk_id>",
		Short: "One task with its derived verdict — the rung and the evidence behind it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			task, err := client.GetTask(cmd.Context(), org, args[0])
			if err != nil {
				return fmt.Errorf("orun task show: %w", err)
			}
			verdict, err := client.GetTaskVerdict(cmd.Context(), org, args[0])
			if err != nil {
				return fmt.Errorf("orun task show: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"task": task, "verdict": verdict})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s  %s\n", task.Key, task.ID)
			if task.TitleMirror != "" {
				fmt.Fprintf(out, "title     %s\n", task.TitleMirror)
			}
			if task.Epic != nil {
				fmt.Fprintf(out, "epic      %s (%s)\n", task.Epic.Slug, orDash(task.Epic.Key))
			}
			if task.Milestone != nil {
				fmt.Fprintf(out, "milestone %s (%s)\n", orDash(task.Milestone.Name), task.Milestone.ID)
			}
			if task.Assignee != "" {
				fmt.Fprintf(out, "assignee  %s\n", task.Assignee)
			}
			if task.Brief != "" {
				fmt.Fprintf(out, "brief     %s\n", task.Brief)
			}
			fmt.Fprintf(out, "rung      %s — %s\n", verdict.Verdict.Rung, verdict.Verdict.Evidence.Reason)
			if verdict.Verdict.Pin != nil {
				fmt.Fprintf(out, "pin       %s (active: %t)\n", verdict.Verdict.Pin.Rung, verdict.Verdict.Pin.Active)
			}
			if verdict.Verdict.Dissent != nil {
				fmt.Fprintf(out, "dissent   asserted %s\n", verdict.Verdict.Dissent.Asserted)
			}
			if verdict.Verdict.Blocked {
				fmt.Fprintf(out, "blocked   by %s\n", strings.Join(verdict.Verdict.BlockedBy, ", "))
			}
			fmt.Fprintf(out, "contract  %s\n", orDash(shortRevision(task.ContractHash)))
			for _, d := range verdict.Dependencies {
				fmt.Fprintf(out, "dep       %s (%s)\n", d.Ref, d.State)
			}
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskCheckCommand() *cobra.Command {
	var (
		baseRef string
		headRef string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "check <key>",
		Short: "Check the repo's contract document offline: validity, completeness, affects vs diff",
		Long: `Check a task's contract without the network — the authoring loop of
design §3.3. Validates tasks/<KEY>.TaskContract.yaml strictly, reports
completeness in the same terms the cloud derives readiness from, and with
--base compares the components the diff actually touched against the
contract's affects ceiling (the same change engine 'plan --changed' uses).

Advisory by construction: the workspace re-decides at enforcement, and
effective access is resolved policy ∩ contract — never wider than either.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskCheck(cmd, args[0], baseRef, headRef, asJSON)
		},
	}
	cmd.Flags().StringVar(&baseRef, "base", "", "base ref: also check the diff's components against affects")
	cmd.Flags().StringVar(&headRef, "head", "", "head ref for --base (default: working tree)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

type taskCheckReport struct {
	Document     string   `json:"document"`
	Key          string   `json:"key"`
	ContractHash string   `json:"contractHash"`
	Complete     bool     `json:"complete"`
	Missing      []string `json:"missing"`
	// Base/Outside are present only when --base ran the change engine.
	Base            string   `json:"base,omitempty"`
	DirectlyChanged []string `json:"directlyChanged,omitempty"`
	Outside         []string `json:"outsideAffects,omitempty"`
	Advisory        string   `json:"advisory"`
}

const taskCheckAdvisory = "advisory: offline check — the workspace decides at enforcement, and effective access is resolved policy ∩ contract (never wider than either)"

func runTaskCheck(cmd *cobra.Command, key, baseRef, headRef string, asJSON bool) error {
	out := cmd.OutOrStdout()
	root := taskDocRoot()
	doc, err := taskfile.FindForKey(root, key)
	if err != nil {
		return fmt.Errorf("orun task check: %w", err)
	}
	if doc == nil {
		fmt.Fprintf(out, "no contract document at %s\n", taskfile.PathFor(root, key))
		fmt.Fprintln(out, "no contract ⇒ no narrowing (the task, if it exists, constrains nothing)")
		return nil
	}
	hash, _, err := contract.ContractID(doc.Contract)
	if err != nil {
		return fmt.Errorf("orun task check: %w", err)
	}
	report := taskCheckReport{
		Document:     doc.Path,
		Key:          doc.Key,
		ContractHash: hash,
		Missing:      taskfile.Missing(doc.Contract),
		Advisory:     taskCheckAdvisory,
	}
	report.Complete = len(report.Missing) == 0

	if baseRef != "" {
		changed, derr := detectChangedComponents(cmd.Context(), baseRef, headRef)
		if derr != nil {
			return derr
		}
		report.Base = baseRef
		report.DirectlyChanged = changed
		allowed := map[string]bool{}
		for _, a := range doc.Contract.Affects {
			allowed[a] = true
		}
		for _, c := range changed {
			if !allowed[c] {
				report.Outside = append(report.Outside, c)
			}
		}
		sort.Strings(report.Outside)
	}

	if asJSON {
		if err := encodeJSON(cmd, report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "document  %s\n", report.Document)
		fmt.Fprintf(out, "sealed    %s\n", report.ContractHash)
		if report.Complete {
			fmt.Fprintln(out, "complete  yes")
		} else {
			fmt.Fprintf(out, "complete  no — missing %s\n", strings.Join(report.Missing, ", "))
		}
		if report.Base != "" {
			if len(report.Outside) == 0 {
				fmt.Fprintf(out, "affects   ok — %d changed component(s), all inside the contract\n", len(report.DirectlyChanged))
			} else {
				fmt.Fprintf(out, "affects   %d component(s) outside the contract: %s\n", len(report.Outside), strings.Join(report.Outside, ", "))
			}
		}
		fmt.Fprintln(out, taskCheckAdvisory)
	}
	if len(report.Outside) > 0 {
		return fmt.Errorf("orun task check: %d changed component(s) outside the contract's affects", len(report.Outside))
	}
	return nil
}

// detectChangedComponents runs the affected engine for the check verb and
// returns the DIRECTLY changed components — what the diff itself touched,
// which is what an affects ceiling is about (dependents rebuild, but the
// contract constrains what the work edits).
func detectChangedComponents(ctx context.Context, baseRef, headRef string) ([]string, error) {
	store, refs, _, err := openObjectModel()
	if err != nil {
		return nil, exitErr(3, "open object model: %w", err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return nil, exitErr(6, "no catalog found; run 'orun catalog refresh' or 'orun plan' first")
		}
		return nil, exitErr(3, "load catalog: %w", err)
	}
	if view.Ownership == nil {
		return nil, exitErr(6, "catalog has no impact index; run 'orun catalog refresh'")
	}
	opts := git.ChangeOptions{Base: baseRef, Head: headRef}
	if verr := git.ValidateOptions(opts); verr != nil {
		return nil, exitErr(1, "invalid change options: %w", verr)
	}
	res, err := affected.NewDetector(&view, affected.IntentImpact("watch")).
		Detect(ctx, affected.GitChangeSource{Options: opts, IntentPath: "intent.yaml"})
	if err != nil {
		return nil, exitErr(2, "change detection: %w", err)
	}
	return res.DirectlyChanged, nil
}

// attachDocumentIfPresent attaches the repo's contract document for a key,
// when one exists. (nil, "", nil) = no document, which is a legal state.
func attachDocumentIfPresent(ctx context.Context, client *remotestate.Client, org, key string) (*taskfile.Document, string, error) {
	doc, err := taskfile.FindForKey(taskDocRoot(), key)
	if err != nil || doc == nil {
		return nil, "", err
	}
	hash, wire, err := contract.ContractID(doc.Contract)
	if err != nil {
		return doc, "", err
	}
	if _, err := client.AttachTaskContract(ctx, org, key, wire, hash); err != nil {
		return doc, hash, err
	}
	return doc, hash, nil
}

// attachTemplate seals an explicit (unbound) contract document and attaches
// it to the just-issued key; the Document handed back is bound to that key
// so the local seal records it like a repo-authored one.
func attachTemplate(ctx context.Context, client *remotestate.Client, org, key string, template *taskfile.Document) (*taskfile.Document, string, error) {
	doc := &taskfile.Document{Key: key, Path: template.Path, Contract: template.Contract}
	hash, err := taskfile.Attach(ctx, client, org, key, doc)
	return doc, hash, err
}

// sealTaskLocally records the issuance (and contract, when present) in the
// local object store — refs/tasks/<key>, the store's first non-derivable
// root (TK-9). Best-effort by design: the cloud holds the identity; a
// missing local store degrades to a note, never a failed create.
func sealTaskLocally(ctx context.Context, task *remotestate.PublicTask, doc *taskfile.Document) string {
	store, refs, _, err := openObjectModel()
	if err != nil {
		return fmt.Sprintf("not recorded in the local object store (%v)", err)
	}
	in := taskobj.SealInput{Key: task.Key, TaskRef: task.ID, Title: task.TitleMirror}
	if doc != nil {
		in.Contract = doc.Contract
	}
	if _, _, err := taskobj.SealTask(ctx, store, refs, in); err != nil {
		return fmt.Sprintf("not recorded in the local object store (%v)", err)
	}
	return fmt.Sprintf("recorded as refs/tasks/%s in the local object store", task.Key)
}

// parseDeriveRef parses --derive's prefix#number form.
func parseDeriveRef(s string) (*remotestate.TaskDerive, error) {
	prefix, num, ok := strings.Cut(s, "#")
	if !ok || prefix == "" {
		return nil, fmt.Errorf("orun task create: --derive wants prefix#number (e.g. web#123), got %q", s)
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("orun task create: --derive issue number %q is not a positive integer", num)
	}
	return &remotestate.TaskDerive{RepoPrefix: prefix, IssueNumber: n}, nil
}
