package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/workbrief"
	"github.com/sourceplane/orun/internal/worklens"
)

func registerSpecCommand(root *cobra.Command) {
	specCmd := &cobra.Command{
		Use:   "spec",
		Short: "Frozen spec snapshots: pull a sealed brief to implement against",
		Long: `Frozen spec snapshots (specs/orun-work v2, the system of proof).

A SpecSnapshot is the intent plane only — the spec doc reference plus task
contracts, pinned to the two log cursors it reflects. It structurally cannot
carry a rung or an assignee; an agent implementing against spec@hash cannot
have the ground shift under it.

Subcommands:
  pull    Freeze a spec's current intent into a content-addressed snapshot

Run 'orun spec <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	registerSpecPullCommand(specCmd)
	root.AddCommand(specCmd)
}

func registerSpecPullCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		idOnly     bool
		pushRemote bool
	)
	cmd := &cobra.Command{
		Use:   "pull <spec-slug>[@sha256:…]",
		Short: "Freeze a spec's intent into a content-addressed snapshot under .orun/specs/",
		Long: `Fetch the spec's current intent (envelope + task contracts) from the
workspace fold API, seal it into a canonical, content-addressed SpecSnapshot,
and materialize a read-only view under .orun/specs/<slug>/.

With an @sha256:… pin the sealed snapshot must match the pin exactly — the
dispatcher's guarantee that an agent implements against exactly this brief.
(v1 seals client-side from the fold query; server-side sealing + the
refs/work remote ride a later slice.)`,
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
			summary, err := client.GetWorkSummary(cmd.Context())
			if err != nil {
				return fmt.Errorf("orun spec pull: %w", err)
			}

			snapshot, err := workbrief.SnapshotFromSummary(client.Scope().OrgID, slug, summary)
			if err != nil {
				return err
			}
			id, canonical, err := worklens.SealSpecSnapshot(*snapshot)
			if err != nil {
				return err
			}
			if pin != "" && pin != id {
				return fmt.Errorf("orun spec pull: snapshot %s does not match pin %s (the spec moved since the pin was minted)", id, pin)
			}

			dir := filepath.Join(".orun", "specs", slug)
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
			if err := os.WriteFile(briefPath, []byte(renderBrief(snapshot, id)), 0o444); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if pushRemote {
				store, refs, _, err := openObjectModel()
				if err != nil {
					return err
				}
				refName, res, err := pushSealedSnapshot(cmd.Context(), store, refs, client, slug, canonical)
				if err != nil {
					return fmt.Errorf("orun spec pull --push: %w", err)
				}
				if !idOnly {
					fmt.Fprintf(out, "pushed  %s (%d copied, %d already present)\n", refName, res.Copied, res.Skipped)
				}
			}
			if idOnly {
				fmt.Fprintln(out, id)
				return nil
			}
			fmt.Fprintf(out, "sealed  %s\n", id)
			fmt.Fprintf(out, "spec    %s — %s (%d tasks)\n", snapshot.Spec.Key, snapshot.Spec.Title, len(snapshot.Tasks))
			fmt.Fprintf(out, "cursors coord=%d obs=%d\n", snapshot.CoordSeq, snapshot.ObsSeq)
			fmt.Fprintf(out, "view    %s (read-only)\n", dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&idOnly, "id-only", false, "print only the snapshot id (for scripting/dispatch)")
	cmd.Flags().BoolVar(&pushRemote, "push", false, "also store the sealed snapshot in the object store and sync refs/work/specs/<slug>/latest to the remote")
	parent.AddCommand(cmd)
}

// remoteEndpoints is the seam the push test fakes: the real client returns
// its org/project-routed remote object+ref stores.
type remoteEndpoints interface {
	RemoteStores() (objectstore.ObjectStore, refstore.RefStore)
}

// pushSealedSnapshot writes the canonical snapshot bytes into the local
// object store as a blob, advances the local work ref, and syncs the closure
// to the remote (set-difference: objects already present are skipped) — the
// same push spine the catalog uses. Sealing stays in Go (one canonicalizer,
// one determinism contract); the remote leg is pure content transport.
func pushSealedSnapshot(
	ctx context.Context,
	store objectstore.ObjectStore,
	refs refstore.RefStore,
	remote remoteEndpoints,
	slug string,
	canonical []byte,
) (string, objremote.Result, error) {
	blobID, err := store.PutBlob(ctx, canonical)
	if err != nil {
		return "", objremote.Result{}, fmt.Errorf("store snapshot blob: %w", err)
	}
	refName := "work/specs/" + slug + "/latest"
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

// renderBrief writes the human/agent-readable face of the frozen snapshot.
func renderBrief(s *worklens.SpecSnapshot, id string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — frozen brief\n\n", s.Spec.Title)
	fmt.Fprintf(&b, "- snapshot: `%s`\n- spec: `%s`\n- cursors: coord=%d obs=%d\n", id, s.Spec.Key, s.CoordSeq, s.ObsSeq)
	if s.Spec.DocRef != "" {
		fmt.Fprintf(&b, "- doc: `%s`\n", s.Spec.DocRef)
	}
	fmt.Fprintf(&b, "\nThis view is read-only by construction: lifecycle derives from the\nobservation log, and coordination happens through the mutators — nothing\nhere accepts an edit.\n")
	keys := make([]string, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		keys = append(keys, t.Key)
	}
	sort.Strings(keys)
	for _, t := range s.Tasks {
		fmt.Fprintf(&b, "\n## %s — %s\n", t.Key, t.Title)
		if c := t.Contract; c != nil {
			if c.Goal != "" {
				fmt.Fprintf(&b, "\n**Goal:** %s\n", c.Goal)
			}
			if len(c.Affects) > 0 {
				fmt.Fprintf(&b, "**Affects:** %s\n", strings.Join(c.Affects, ", "))
			}
			if len(c.DoneWhen) > 0 {
				fmt.Fprintf(&b, "**Done when:**\n")
				for _, d := range c.DoneWhen {
					fmt.Fprintf(&b, "- %s\n", d)
				}
			}
			if len(c.Gates) > 0 {
				fmt.Fprintf(&b, "**Gates:** %s\n", strings.Join(c.Gates, ", "))
			}
			if len(c.Deps) > 0 {
				fmt.Fprintf(&b, "**Deps:** %s\n", strings.Join(c.Deps, ", "))
			}
		}
	}
	return b.String()
}
