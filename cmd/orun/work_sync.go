package main

// orun work sync (orun-cloud saas-work-gitops WG3, design §6–§7): reconcile
// the declared work tree into the platform. Git declares — existence,
// titles, order, exit criteria, task membership, contracts, docs, the
// epic's state — and the platform runs; the sync creates and updates what
// the declaration names and never deletes anything. Every write carries an
// Idempotency-Key of the form work:<repo>:<path>:<sha>[:<what>], so a re-run
// of the same commit is a no-op at the edge, and the X-Orun-Work-Sync
// header, which the platform stamps as the epic's managedBy pointer.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workfile"
)

// stateRank orders the native states for the forward-only rule (design §7):
// a declared state applies only when it does not move an epic backwards,
// and a terminal epic never moves again from git.
var stateRank = map[string]int{"backlog": 0, "started": 1, "paused": 1, "completed": 2, "canceled": 2}

type workSyncAction struct {
	Verb   string `json:"verb"` // create | update | attach | push | skip | warn
	What   string `json:"what"`
	Detail string `json:"detail,omitempty"`
}

// workSyncer holds one run's state. platform is nil under --dry-run past
// the reads it needs; every write goes through one of the do* methods so
// the plan and the run print the same lines.
type workSyncer struct {
	ctx     context.Context
	client  *remotestate.Client
	org     string
	repo    string
	sha     string
	dryRun  bool
	out     io.Writer
	actions []workSyncAction
	writes  int
	warns   int
}

func (s *workSyncer) say(verb, what, detail string) {
	s.actions = append(s.actions, workSyncAction{Verb: verb, What: what, Detail: detail})
	if verb == "warn" {
		s.warns++
	}
	prefix := verb
	if s.dryRun && verb != "warn" && verb != "skip" {
		prefix = "would " + verb
	}
	if detail != "" {
		fmt.Fprintf(s.out, "%-14s %s — %s\n", prefix, what, detail)
	} else {
		fmt.Fprintf(s.out, "%-14s %s\n", prefix, what)
	}
}

// authoringChannel names the channel a refused create points to.
func authoringChannel(kind string) string {
	switch kind {
	case "tracker":
		return "its tracker"
	case "platform":
		return "the console"
	case "":
		return "another channel"
	}
	return kind
}

// idem builds the Idempotency-Key for one write: repo, path and commit in
// the clear (what a reader greps for), then a short digest of WHAT — the
// milestone name, the change list — because those carry dashes, arrows and
// spaces, and the edge accepts only printable ASCII in the header.
func (s *workSyncer) idem(path, what string) string {
	sum := sha256.Sum256([]byte(what))
	return "work:" + s.repo + ":" + path + ":" + s.sha + ":" + hex.EncodeToString(sum[:8])
}

func newWorkSyncCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		dryRun     bool
		force      bool
		repoName   string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile the declared work tree into the platform (CI, on the default branch)",
		Long: `Read every epic.yaml under work.epics and make the platform agree with it:
create the epic by slug, set its title, summary and state (forward only),
create and order its milestones, create each listed task with its contract
attached (adopting the key the declaration chose), push every doc. Nothing
is ever deleted: a milestone or task the tree no longer names is reported,
not removed. Every write is idempotent by repo, path and commit, so the
job can run on every push to main.

Runs only when intent.yaml says 'sync: on-merge' (or with --force);
--dry-run prints the plan and writes nothing, whatever intent says.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			layout := loadIntentForCloudConfig().WorkLayout()
			if !layout.Declared() {
				return fmt.Errorf("orun work sync: intent.yaml declares no work section — nothing to sync")
			}
			if layout.Sync != model.WorkSyncOnMerge && !dryRun && !force {
				return fmt.Errorf("orun work sync: intent.yaml work.sync is %q — set it to on-merge, or pass --dry-run to see the plan", layout.Sync)
			}
			root := taskDocRoot()
			// The tree is judged before any network: a broken declaration is
			// refused whether or not a workspace resolves.
			tree, err := loadWorkTree(root, layout)
			var problems *workTreeProblems
			if errors.As(err, &problems) {
				for _, p := range problems.Problems {
					fmt.Fprintf(cmd.ErrOrStderr(), "error work-manifest   %s\n", p)
				}
				return exitErr(1, "orun work sync: the tree has %d problem(s) — `orun work check` names them; nothing was written", len(problems.Problems))
			}
			if err != nil {
				return fmt.Errorf("orun work sync: %w", err)
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				out = io.Discard
			}
			s, err := runWorkSyncTree(cmd.Context(), root, tree, client, repoName, dryRun, out)
			if err != nil && s == nil {
				return fmt.Errorf("orun work sync: %w", err)
			}
			if err != nil {
				if asJSON {
					_ = encodeJSON(cmd, map[string]any{"repo": s.repo, "sha": s.sha, "dryRun": dryRun, "actions": s.actions, "error": err.Error()})
				}
				return exitErr(1, "orun work sync: %v", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"repo": s.repo, "sha": s.sha, "dryRun": dryRun, "actions": s.actions, "writes": s.writes, "warnings": s.warns})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan; write nothing")
	cmd.Flags().BoolVar(&force, "force", false, "run even when intent.yaml work.sync is off")
	cmd.Flags().StringVar(&repoName, "repo", "", "repository identity for the docs and the managedBy pointer (default: from the origin remote)")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// workTreeProblems is the error a sync answers when the tree fails the
// work-manifest rules: nothing was written, and each line names a file.
type workTreeProblems struct{ Problems []string }

func (e *workTreeProblems) Error() string {
	return fmt.Sprintf("the tree has %d problem(s)", len(e.Problems))
}

// runWorkSyncTree is one sync of a loaded tree at root — what `orun work
// sync` and the plan-time auto-sync share. It sets the sync header for HEAD, reconciles every epic in order, and prints the
// summary line to out. The syncer is returned even on a failed write, so a
// caller can report what was applied before it; the error says so too.
// loadWorkTree loads and judges the declared tree: a parse failure is the
// error, a rule violation is a *workTreeProblems.
func loadWorkTree(root string, layout model.WorkLayout) (*workfile.Tree, error) {
	tree, err := workfile.Load(root, layout)
	if err != nil {
		return nil, err
	}
	if len(tree.Problems) > 0 {
		return nil, &workTreeProblems{Problems: tree.Problems}
	}
	return tree, nil
}

func runWorkSyncTree(ctx context.Context, root string, tree *workfile.Tree, client *remotestate.Client, identity string, dryRun bool, out io.Writer) (*workSyncer, error) {
	sha, err := gitOutIn(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s has no commits yet", root)
	}
	if identity == "" {
		identity = specRepoIdentity(root)
	}
	client.SetHeader(remotestate.WorkSyncHeader, identity+"@"+sha)
	s := &workSyncer{ctx: ctx, client: client, org: client.Scope().OrgID, repo: identity, sha: sha, dryRun: dryRun, out: out}
	for _, e := range tree.Epics {
		if err := s.syncEpic(root, tree, e); err != nil {
			return s, fmt.Errorf("%s: %v — %d write(s) applied before it; the next sync resumes from the tree", e.Slug, err, s.writes)
		}
	}
	if dryRun {
		fmt.Fprintf(s.out, "dry run: %d write(s) would be made, %d warning(s)\n", s.writes, s.warns)
	} else {
		fmt.Fprintf(s.out, "synced %d epic(s) at %s: %d write(s), %d warning(s)\n", len(tree.Epics), shortRevision(sha), s.writes, s.warns)
	}
	return s, nil
}

// syncEpic reconciles one declaration, in the order design §6 lists.
func (s *workSyncer) syncEpic(root string, tree *workfile.Tree, e *workfile.Epic) error {
	// 1. The epic, by slug.
	var remote *remotestate.PublicEpic
	var milestones []remotestate.PublicMilestone
	view, err := s.client.GetEpic(s.ctx, s.org, e.Slug)
	switch {
	case err == nil:
		remote = &view.Epic
		milestones = view.Milestones
	case remotestate.IsNotFound(err):
		s.say("create", "epic "+e.Slug, e.Title)
		if !s.dryRun {
			created, err := s.client.CreateEpicWithKey(s.ctx, s.org, remotestate.EpicCreateRequest{
				Name: e.Title, Slug: e.Slug, Description: e.Summary, TargetDate: e.TargetDate, Owner: e.Owner, KeyPrefix: e.Key,
			}, s.idem(e.Path, "epic"))
			if kind, refused := remotestate.AuthoredElsewhereOf(err); refused {
				// The workspace creates new work elsewhere (its Work setting):
				// the declaration waits, the rest of the tree still syncs.
				s.say("warn", "epic "+e.Slug, "not created — this workspace creates new work in "+authoringChannel(kind)+" (Settings → Work); skipped")
				return nil
			}
			if err != nil {
				return fmt.Errorf("create epic: %w", err)
			}
			s.writes++
			remote = created
		} else {
			s.writes++
			remote = &remotestate.PublicEpic{Slug: e.Slug, StateCategory: "backlog"}
		}
	default:
		return fmt.Errorf("read epic: %w", err)
	}
	if remote.Provider != "" {
		s.say("warn", "epic "+e.Slug, "is mirrored from "+remote.Provider+" — a tracker's epic is never managed from git; skipped")
		return nil
	}

	// 2. Declared fields, when they differ. Owner is set only when the
	// platform has none ("me" cannot be compared with a usr_… id).
	upd := remotestate.EpicUpdateRequest{}
	changes := []string{}
	if e.Title != remote.Name {
		upd.Name, changes = e.Title, append(changes, "title")
	}
	if e.Summary != "" && e.Summary != remote.Description {
		upd.Description, changes = e.Summary, append(changes, "summary")
	}
	if e.TargetDate != "" && e.TargetDate != remote.TargetDate {
		upd.TargetDate, changes = e.TargetDate, append(changes, "targetDate")
	}
	if e.Owner != "" && remote.Owner == "" {
		upd.Owner, changes = e.Owner, append(changes, "owner")
	}
	// 3. State, forward only.
	if have, want := stateRank[remote.StateCategory], stateRank[e.State]; e.State != remote.StateCategory {
		switch {
		case have >= 2:
			s.say("skip", "epic "+e.Slug+" state", fmt.Sprintf("the platform says %s, a terminal state; git says %s — reopening is a platform action", remote.StateCategory, e.State))
		case want < have:
			s.say("skip", "epic "+e.Slug+" state", fmt.Sprintf("git says %s but the platform is already at %s — state moves forward only", e.State, remote.StateCategory))
		default:
			upd.State, changes = e.State, append(changes, "state → "+e.State)
		}
	}
	// The prefix the declaration reserves rides every write that stamps —
	// an epic adopted from the console has none until its first sync.
	if remote.ID != "" && (remote.ManagedBy == nil || remote.ManagedBy.KeyPrefix != e.Key) {
		upd.KeyPrefix, changes = e.Key, append(changes, "keyPrefix "+e.Key)
	}
	if len(changes) > 0 {
		s.say("update", "epic "+e.Slug, strings.Join(changes, ", "))
		s.writes++
		if !s.dryRun {
			if _, err := s.client.UpdateEpicWithKey(s.ctx, s.org, e.Slug, upd, s.idem(e.Path, "epic:"+strings.Join(changes, ","))); err != nil {
				return fmt.Errorf("update epic: %w", err)
			}
		}
	} else if remote.ID != "" && !pointsAt(remote.ManagedBy, s.repo, s.sha) {
		// Design §6: the pointer names the commit the tree was last read at,
		// and every run moves it — a run that changes no declared field still
		// tells the platform which commit it agrees with. An empty PATCH under
		// the sync header is that stamp and nothing else (the platform writes
		// no field for it).
		s.say("stamp", "epic "+e.Slug, "managedBy — "+s.repo+"@"+shortSHA(s.sha))
		s.writes++
		if !s.dryRun {
			if _, err := s.client.UpdateEpicWithKey(s.ctx, s.org, e.Slug, remotestate.EpicUpdateRequest{}, s.idem(e.Path, "epic:stamp")); err != nil {
				return fmt.Errorf("stamp epic: %w", err)
			}
		}
	}

	// 4. Milestones: match by name, create in order, re-order, never delete.
	sort.SliceStable(milestones, func(i, j int) bool { return milestones[i].SortOrder < milestones[j].SortOrder })
	byName := map[string]*remotestate.PublicMilestone{}
	for i := range milestones {
		byName[milestones[i].Name] = &milestones[i]
	}
	ids := make([]string, len(e.Milestones)) // the id of each declared phase, in declared order
	prevID := ""
	for i, m := range e.Milestones {
		if rm, ok := byName[m.Name]; ok {
			ids[i] = rm.ID
			req := remotestate.MilestoneUpdateRequest{}
			changes := []string{}
			if !sameStrings(rm.ExitCriteria, m.ExitCriteria) && (len(rm.ExitCriteria) > 0 || len(m.ExitCriteria) > 0) {
				req.ExitCriteria, changes = m.ExitCriteria, append(changes, "exitCriteria")
				if req.ExitCriteria == nil {
					req.ExitCriteria = []string{}
				}
			}
			if pos := indexOfMilestone(milestones, rm.ID); (i == 0 && pos != 0) || (i > 0 && (pos == 0 || milestones[pos-1].ID != prevID)) {
				changes = append(changes, "position")
				if i == 0 {
					req.AfterFirst = true
				} else {
					req.After = prevID
				}
			}
			if len(changes) > 0 {
				s.say("update", fmt.Sprintf("milestone %q", m.Name), strings.Join(changes, ", "))
				s.writes++
				if !s.dryRun {
					if _, err := s.client.UpdateMilestoneWithKey(s.ctx, s.org, rm.ID, req, s.idem(e.Path, "milestone:"+m.Name+":"+strings.Join(changes, ","))); err != nil {
						return fmt.Errorf("update milestone %q: %w", m.Name, err)
					}
					// keep the local picture in declared order for the next position check
					milestones = moveAfter(milestones, rm.ID, prevID)
				}
			}
		} else {
			s.say("create", fmt.Sprintf("milestone %q", m.Name), positionWord(i, prevID))
			s.writes++
			if !s.dryRun {
				req := remotestate.MilestoneCreateRequest{Name: m.Name, ExitCriteria: m.ExitCriteria}
				if i == 0 {
					req.First = len(milestones) > 0
				} else {
					req.After = prevID
				}
				created, err := s.client.CreateMilestoneWithKey(s.ctx, s.org, e.Slug, req, s.idem(e.Path, "milestone:"+m.Name))
				if err != nil {
					return fmt.Errorf("create milestone %q: %w", m.Name, err)
				}
				ids[i] = created.ID
				milestones = insertAfter(milestones, *created, prevID)
			} else {
				ids[i] = "mls_(new:" + m.Name + ")"
			}
		}
		prevID = ids[i]
	}
	declared := map[string]bool{}
	for _, m := range e.Milestones {
		declared[m.Name] = true
	}
	for _, rm := range milestones {
		if !declared[rm.Name] {
			s.say("warn", fmt.Sprintf("milestone %q", rm.Name), "exists on the platform but "+e.Path+" does not declare it — left as is (the sync never deletes)")
		}
	}

	// 5. Tasks: adopt the declared key, attach the contract, place in the milestone.
	for i, m := range e.Milestones {
		for _, key := range m.Tasks {
			doc := tree.Contracts[key]
			hash, wire, err := contract.ContractID(doc.Contract)
			if err != nil {
				return fmt.Errorf("%s: %w", doc.Path, err)
			}
			title := doc.Title
			if title == "" {
				title = firstSentence(doc.Contract.Goal)
			}
			task, err := s.client.GetTask(s.ctx, s.org, key)
			switch {
			case err == nil:
				if task.Milestone == nil || task.Milestone.ID != ids[i] {
					where := "no milestone"
					if task.Milestone != nil {
						where = task.Milestone.ID
					}
					s.say("warn", "task "+key, fmt.Sprintf("sits in %s on the platform but %q in %s — moving a task is a platform action today", where, m.Name, e.Path))
				}
				if task.ContractHash != hash {
					s.say("attach", "contract "+key, shortRevision(hash))
					s.writes++
					if !s.dryRun {
						if _, err := s.client.AttachTaskContractWithKey(s.ctx, s.org, key, wire, hash, s.idem(doc.Path, "contract:"+hash)); err != nil {
							return fmt.Errorf("attach %s: %w", key, err)
						}
					}
				}
			case remotestate.IsNotFound(err):
				s.say("create", "task "+key, fmt.Sprintf("%q in %q", title, m.Name))
				s.writes += 2
				if !s.dryRun {
					created, err := s.client.CreateTaskWithKey(s.ctx, s.org, remotestate.TaskCreateRequest{
						AdoptKey: key, MintPrefix: e.Key, TitleMirror: title, Brief: doc.Contract.Goal, Milestone: ids[i],
					}, s.idem(doc.Path, "task"))
					if err != nil {
						return fmt.Errorf("create %s: %w", key, err)
					}
					if created.Key != key {
						return fmt.Errorf("create %s: the platform issued %s instead — the prefix %s is not reserved for this tree", key, created.Key, e.Key)
					}
					if _, err := s.client.AttachTaskContractWithKey(s.ctx, s.org, key, wire, hash, s.idem(doc.Path, "contract:"+hash)); err != nil {
						return fmt.Errorf("attach %s: %w", key, err)
					}
				}
			default:
				return fmt.Errorf("read %s: %w", key, err)
			}
		}
	}

	// 6. Docs: the committed copy of every file the globs name.
	seen := map[string]bool{}
	for _, glob := range e.Docs {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(e.Dir), filepath.FromSlash(glob)))
		sort.Strings(matches)
		for _, abs := range matches {
			if seen[abs] || filepath.Base(abs) == workfile.EpicFile {
				continue
			}
			seen[abs] = true
			file, _, err := resolveSpecFile(abs)
			if err != nil {
				return err
			}
			if s.dryRun {
				s.say("push", "doc "+file.Slug, file.RelPath)
				s.writes++
				continue
			}
			seal, err := s.client.PushEpicDoc(s.ctx, s.org, e.Slug, file.Slug, remotestate.EpicDocPushRequest{
				Repo: s.repo, Path: file.RelPath, Sha: file.Sha, Content: file.Content, Title: file.Title,
			})
			if err != nil {
				return fmt.Errorf("push %s: %w", file.RelPath, err)
			}
			if seal.Updated {
				s.say("push", "doc "+file.Slug, file.RelPath+" ("+shortRevision(seal.ContentHash)+")")
				s.writes++
			} else {
				s.say("skip", "doc "+file.Slug, "unchanged")
			}
		}
	}
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOfMilestone(ms []remotestate.PublicMilestone, id string) int {
	for i, m := range ms {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func moveAfter(ms []remotestate.PublicMilestone, id, afterID string) []remotestate.PublicMilestone {
	i := indexOfMilestone(ms, id)
	if i < 0 {
		return ms
	}
	m := ms[i]
	ms = append(ms[:i:i], ms[i+1:]...)
	return insertAfter(ms, m, afterID)
}

func insertAfter(ms []remotestate.PublicMilestone, m remotestate.PublicMilestone, afterID string) []remotestate.PublicMilestone {
	at := 0
	if afterID != "" {
		at = indexOfMilestone(ms, afterID) + 1
	}
	out := make([]remotestate.PublicMilestone, 0, len(ms)+1)
	out = append(out, ms[:at]...)
	out = append(out, m)
	return append(out, ms[at:]...)
}

func positionWord(i int, prevID string) string {
	if i == 0 {
		return "first"
	}
	return "after " + prevID
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".!?\n"); i > 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// pointsAt reports whether a managed pointer already names this repository
// at this commit — the case in which a run has nothing to stamp.
func pointsAt(m *remotestate.EpicManagedBy, repo, sha string) bool {
	return m != nil && m.Repo == repo && m.SHA == sha
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
