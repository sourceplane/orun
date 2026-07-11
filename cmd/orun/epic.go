package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/worklens"
)

// orun epic pull (orun-work-v4 WH4) — the strict superset of `orun spec
// pull`. Where spec pull seals client-side from the fold summary, epic pull
// fetches the brief the APPROVAL sealed: the cloud freezes the EpicSnapshot
// (envelope + milestone ladder + ladderHash + task contracts + cursors) in
// the same transaction as the `approved` event, so the id in the approved
// payload IS the artifact an agent implements against. Verification is
// content addressing itself: sha256(bytes) must equal the id — no second
// canonicalizer exists to drift (V4-6).

func registerEpicCommand(root *cobra.Command) {
	epicCmd := &cobra.Command{
		Use:   "epic",
		Short: "Frozen epic briefs: pull the snapshot an approval sealed",
		Long: `Frozen epic briefs (specs/orun-work-v4, cluster WH).

An EpicSnapshot ⊇ SpecSnapshot: the epic document reference, the milestone
ladder and its canonical hash, the task contracts, and the approval record —
sealed by the approve mutator in the same transaction as the approved event.
Approval is the dispatch artifact: what you pull is exactly what a human
approved.

Subcommands:
  pull    Fetch and verify the sealed brief for an approved epic

Run 'orun epic <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	registerEpicPullCommand(epicCmd)
	root.AddCommand(epicCmd)
}

func registerEpicPullCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		idOnly     bool
		pushRemote bool
	)
	cmd := &cobra.Command{
		Use:   "pull <epic-slug>[@sha256:…]",
		Short: "Fetch the approval-sealed EpicSnapshot into a read-only view under .orun/epics/",
		Long: `Fetch the sealed brief for an approved epic, verify its content id by
hashing the returned canonical bytes, and materialize a read-only view under
.orun/epics/<slug>/.

With an @sha256:… pin the brief must be exactly that snapshot — the
dispatcher's guarantee that an agent implements against exactly what was
approved. An unapproved epic has no brief (approval seals one; design §3).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, pin := args[0], ""
			if i := strings.Index(args[0], "@"); i >= 0 {
				slug, pin = args[0][:i], args[0][i+1:]
			}

			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			brief, err := client.GetEpicBrief(cmd.Context(), slug, pin)
			if err != nil {
				return fmt.Errorf("orun epic pull: %w", err)
			}
			canonical := []byte(brief.Canonical)
			if err := worklens.VerifySealedBytes(brief.ID, canonical); err != nil {
				return fmt.Errorf("orun epic pull: %w", err)
			}
			if pin != "" && pin != brief.ID {
				return fmt.Errorf("orun epic pull: brief %s does not match pin %s", brief.ID, pin)
			}
			var snapshot worklens.EpicSnapshot
			if err := json.Unmarshal(canonical, &snapshot); err != nil {
				return fmt.Errorf("orun epic pull: parse sealed brief: %w", err)
			}
			if snapshot.Kind != "EpicSnapshot" {
				return fmt.Errorf("orun epic pull: sealed object is a %s, not an EpicSnapshot", snapshot.Kind)
			}

			dir := filepath.Join(".orun", "epics", slug)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			snapPath := filepath.Join(dir, "snapshot.json")
			_ = os.Chmod(snapPath, 0o644) // allow overwrite of a prior read-only pull
			if err := os.WriteFile(snapPath, canonical, 0o444); err != nil {
				return err
			}
			briefPath := filepath.Join(dir, "BRIEF.md")
			_ = os.Chmod(briefPath, 0o644)
			if err := os.WriteFile(briefPath, []byte(renderEpicBrief(&snapshot, brief.ID)), 0o444); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if pushRemote {
				store, refs, _, err := openObjectModel()
				if err != nil {
					return err
				}
				refName, res, err := pushSealedEpicBrief(cmd.Context(), store, refs, client, slug, canonical)
				if err != nil {
					return fmt.Errorf("orun epic pull --push: %w", err)
				}
				if !idOnly {
					fmt.Fprintf(out, "pushed  %s (%d copied, %d already present)\n", refName, res.Copied, res.Skipped)
				}
			}
			if idOnly {
				fmt.Fprintln(out, brief.ID)
				return nil
			}
			fmt.Fprintf(out, "sealed  %s\n", brief.ID)
			fmt.Fprintf(out, "epic    %s — %s (%d milestones, %d tasks)\n",
				snapshot.Spec.Key, snapshot.Spec.Title, len(snapshot.Milestones), len(snapshot.Tasks))
			fmt.Fprintf(out, "approved by %s%s\n", snapshot.Approval.By.ID, approvedRevisionSuffix(snapshot))
			fmt.Fprintf(out, "cursors coord=%d obs=%d\n", snapshot.CoordSeq, snapshot.ObsSeq)
			fmt.Fprintf(out, "view    %s (read-only)\n", dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&idOnly, "id-only", false, "print only the snapshot id (for scripting/dispatch)")
	cmd.Flags().BoolVar(&pushRemote, "push", false, "also store the sealed brief in the object store and sync refs/work/epics/<slug>/latest to the remote")
	parent.AddCommand(cmd)
}

func approvedRevisionSuffix(s worklens.EpicSnapshot) string {
	if s.Approval.Revision == "" {
		return ""
	}
	return " at " + s.Approval.Revision
}

// pushSealedEpicBrief mirrors pushSealedSnapshot on the epic ref namespace:
// blob into the local object store, advance refs/work/epics/<slug>/latest,
// set-difference-sync the closure to the remote.
func pushSealedEpicBrief(
	ctx context.Context,
	store objectstore.ObjectStore,
	refs refstore.RefStore,
	remote remoteEndpoints,
	slug string,
	canonical []byte,
) (string, objremote.Result, error) {
	blobID, err := store.PutBlob(ctx, canonical)
	if err != nil {
		return "", objremote.Result{}, fmt.Errorf("store brief blob: %w", err)
	}
	refName := "work/epics/" + slug + "/latest"
	oldTarget := ""
	if cur, err := refs.Read(ctx, refName); err == nil {
		oldTarget = cur.Target
	}
	if oldTarget != string(blobID) {
		if err := refs.Update(ctx, refName, oldTarget, string(blobID)); err != nil {
			return "", objremote.Result{}, fmt.Errorf("advance %s: %w", refName, err)
		}
	}
	remoteStore, remoteRefs := remote.RemoteStores()
	res, err := objremote.Sync(ctx,
		objremote.Endpoint{Objects: store, Refs: refs},
		objremote.Endpoint{Objects: remoteStore, Refs: remoteRefs},
		refName,
	)
	if err != nil {
		return "", res, fmt.Errorf("sync %s: %w", refName, err)
	}
	return refName, res, nil
}

// renderEpicBrief writes the human/agent-readable face of the frozen brief:
// the document reference, the milestone ladder with goals and done-when,
// and every task contract — everything an agent needs, nothing it can edit.
func renderEpicBrief(s *worklens.EpicSnapshot, id string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — frozen epic brief\n\n", s.Spec.Title)
	fmt.Fprintf(&b, "- snapshot: `%s`\n- epic: `%s`\n- cursors: coord=%d obs=%d\n", id, s.Spec.Key, s.CoordSeq, s.ObsSeq)
	if s.Spec.DocRef != "" {
		fmt.Fprintf(&b, "- doc: `%s`\n", s.Spec.DocRef)
	}
	fmt.Fprintf(&b, "- ladder: `%s`\n", s.LadderHash)
	fmt.Fprintf(&b, "- approved by `%s`", s.Approval.By.ID)
	if s.Approval.At != "" {
		fmt.Fprintf(&b, " at %s", s.Approval.At)
	}
	b.WriteString("\n\nApproval covers the document and the milestone ladder; tasks are\nregenerable implementation detail (V4-5). This view is read-only by\nconstruction: lifecycle derives from the observation log, and coordination\nhappens through the mutators — nothing here accepts an edit.\n")

	for _, m := range s.Milestones {
		fmt.Fprintf(&b, "\n## %s — %s\n", m.Key, m.Title)
		if m.Goal != "" {
			fmt.Fprintf(&b, "\n**Goal:** %s\n", m.Goal)
		}
		if len(m.DoneWhen) > 0 {
			fmt.Fprintf(&b, "**Done when:**\n")
			for _, d := range m.DoneWhen {
				fmt.Fprintf(&b, "- %s\n", d)
			}
		}
		if m.TargetDate != "" {
			fmt.Fprintf(&b, "**Target:** %s\n", m.TargetDate)
		}
		for _, t := range s.Tasks {
			if t.Milestone != m.Key {
				continue
			}
			fmt.Fprintf(&b, "\n### %s — %s\n", t.Key, t.Title)
			writeContract(&b, t)
		}
	}
	var unscheduled []worklens.Task
	for _, t := range s.Tasks {
		if t.Milestone == "" {
			unscheduled = append(unscheduled, t)
		}
	}
	if len(unscheduled) > 0 {
		b.WriteString("\n## Unscheduled\n")
		for _, t := range unscheduled {
			fmt.Fprintf(&b, "\n### %s — %s\n", t.Key, t.Title)
			writeContract(&b, t)
		}
	}
	return b.String()
}

func writeContract(b *strings.Builder, t worklens.Task) {
	c := t.Contract
	if c == nil {
		return
	}
	if c.Goal != "" {
		fmt.Fprintf(b, "\n**Goal:** %s\n", c.Goal)
	}
	if len(c.Affects) > 0 {
		fmt.Fprintf(b, "**Affects:** %s\n", strings.Join(c.Affects, ", "))
	}
	if len(c.DoneWhen) > 0 {
		fmt.Fprintf(b, "**Done when:**\n")
		for _, d := range c.DoneWhen {
			fmt.Fprintf(b, "- %s\n", d)
		}
	}
	if len(c.Gates) > 0 {
		fmt.Fprintf(b, "**Gates:** %s\n", strings.Join(c.Gates, ", "))
	}
	if len(c.Deps) > 0 {
		fmt.Fprintf(b, "**Deps:** %s\n", strings.Join(c.Deps, ", "))
	}
}
