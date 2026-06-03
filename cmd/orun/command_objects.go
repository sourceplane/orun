package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objgc"
	"github.com/sourceplane/orun/internal/objindex"
	"github.com/sourceplane/orun/internal/objmigrate"
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/workingview"
	"github.com/spf13/cobra"
)

// command_objects.go is the M6 porcelain for the content-addressed object graph
// written under .orun/objectmodel/ (the ORUN_OBJECT_MODEL experiment). The
// commands are hidden until the object model becomes the default at the M12
// cutover. Each command's core is a `runObjects*` function that takes an already
// opened store/refs so it is unit-testable without CLI plumbing.

func registerObjectsCommand(root *cobra.Command) {
	objectsCmd := &cobra.Command{
		Use:   "objects",
		Short: "Inspect the content-addressed object graph",
	}

	catCmd := &cobra.Command{
		Use:   "cat <id|ref>",
		Short: "Print an object's body (JSON pretty-printed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsCat(cmd.Context(), store, refs, args[0], cmd.OutOrStdout())
		},
	}

	lsTreeCmd := &cobra.Command{
		Use:   "ls-tree <id|ref>",
		Short: "List a tree object's entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsLsTree(cmd.Context(), store, refs, args[0], cmd.OutOrStdout())
		},
	}

	revParseCmd := &cobra.Command{
		Use:   "rev-parse <ref>",
		Short: "Resolve a ref name (or id) to an object id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsRevParse(cmd.Context(), refs, args[0], cmd.OutOrStdout())
		},
	}

	fsckCmd := &cobra.Command{
		Use:   "fsck",
		Short: "Verify object integrity and ref closure completeness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsFsck(cmd.Context(), store, refs, cmd.OutOrStdout())
		},
	}

	checkoutCmd := &cobra.Command{
		Use:   "checkout [ref]",
		Short: "Materialize a readable checkout of an object closure",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			ref := "revisions/latest"
			if len(args) == 1 {
				ref = args[0]
			}
			return runObjectsCheckout(cmd.Context(), store, refs, root, ref, cmd.OutOrStdout())
		},
	}

	logCmd := &cobra.Command{
		Use:   "log",
		Short: "List executions newest-first (live runs first, then sealed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsLog(cmd.Context(), store, refs, root, cmd.OutOrStdout())
		},
	}

	showCmd := &cobra.Command{
		Use:   "show [ref]",
		Short: "Show one execution's jobs/steps (live working tree or sealed)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			ref := "executions/latest"
			if len(args) > 0 {
				ref = args[0]
			}
			return runObjectsShow(cmd.Context(), store, refs, root, ref, cmd.OutOrStdout())
		},
	}

	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the derived indexes from refs + objects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsReindex(cmd.Context(), store, refs, root, cmd.OutOrStdout())
		},
	}

	var (
		gcDryRun bool
		gcKeep   int
		gcGrace  time.Duration
	)
	gcCmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect unreachable objects (reachability mark-sweep + retention)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsGC(cmd.Context(), store, refs, root, objgc.Options{
				KeepExecutions: gcKeep,
				GracePeriod:    gcGrace,
				DryRun:         gcDryRun,
			}, cmd.OutOrStdout())
		},
	}
	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "Report what would be removed without deleting")
	gcCmd.Flags().IntVar(&gcKeep, "keep", 0, "Retain only the newest N executions (0 = keep all)")
	gcCmd.Flags().DurationVar(&gcGrace, "grace", time.Hour, "Never sweep objects written within this window")

	var migrateDryRun bool
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Ingest the legacy .orun/ state into the object graph (additive, idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsMigrate(cmd.Context(), state.NewStore(storeDir()), store, refs, migrateDryRun, cmd.OutOrStdout())
		},
	}
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Report what would be ingested without writing")

	pushCmd := &cobra.Command{
		Use:   "push <remote-dir> [ref]",
		Short: "Push a ref's object closure to a remote (file://) store",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			remote, err := openRemoteEndpoint(args[0])
			if err != nil {
				return err
			}
			return runObjectsSync(cmd.Context(), objremote.Endpoint{Objects: store, Refs: refs}, remote, refArg(args), true, cmd.OutOrStdout())
		},
	}
	pullCmd := &cobra.Command{
		Use:   "pull <remote-dir> [ref]",
		Short: "Pull a ref's object closure from a remote (file://) store",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, _, err := openObjectModel()
			if err != nil {
				return err
			}
			remote, err := openRemoteEndpoint(args[0])
			if err != nil {
				return err
			}
			return runObjectsSync(cmd.Context(), objremote.Endpoint{Objects: store, Refs: refs}, remote, refArg(args), false, cmd.OutOrStdout())
		},
	}

	objectsCmd.AddCommand(catCmd, lsTreeCmd, revParseCmd, fsckCmd, checkoutCmd, logCmd, showCmd, reindexCmd, gcCmd, migrateCmd, pushCmd, pullCmd)
	root.AddCommand(objectsCmd)
}

// refArg returns the ref name from a push/pull arg list (default executions/latest).
func refArg(args []string) string {
	if len(args) == 2 {
		return args[1]
	}
	return "executions/latest"
}

// openRemoteEndpoint opens a file:// remote (an object + ref store at dir).
func openRemoteEndpoint(dir string) (objremote.Endpoint, error) {
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: dir})
	if err != nil {
		return objremote.Endpoint{}, fmt.Errorf("open remote object store: %w", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: dir, Writer: "cli"})
	if err != nil {
		return objremote.Endpoint{}, fmt.Errorf("open remote ref store: %w", err)
	}
	return objremote.Endpoint{Objects: store, Refs: refs}, nil
}

func runObjectsSync(ctx context.Context, local, remote objremote.Endpoint, ref string, push bool, out io.Writer) error {
	var (
		res objremote.Result
		err error
	)
	verb := "pulled"
	if push {
		res, err = objremote.Push(ctx, local, remote, ref)
		verb = "pushed"
	} else {
		res, err = objremote.Pull(ctx, local, remote, ref)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s: closure=%d copied=%d skipped=%d ref-moved=%v\n",
		verb, ref, res.Closure, res.Copied, res.Skipped, res.RefMoved)
	return nil
}

func runObjectsMigrate(ctx context.Context, legacy *state.Store, store objectstore.ObjectStore, refs refstore.RefStore, dryRun bool, out io.Writer) error {
	res, err := objmigrate.Migrate(ctx, legacy, store, refs, dryRun)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "migrate: plans=%d executions=%d orphan-executions=%d dry-run=%v\n",
		res.Plans, res.Executions, res.OrphanExecutions, res.DryRun)
	return nil
}

func runObjectsGC(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root string, opts objgc.Options, out io.Writer) error {
	ix := objindex.New(store, refs, root)
	res, err := objgc.Collect(ctx, store, refs, ix, opts)
	if err != nil {
		return err
	}
	if !opts.DryRun {
		// Retention may have pruned executions; refresh the derived index.
		_ = ix.Reindex(ctx)
	}
	fmt.Fprintf(out, "gc: scanned=%d marked=%d pruned-exec-refs=%d swept=%d skipped-grace=%d dry-run=%v\n",
		res.Scanned, res.Marked, res.PrunedExecRefs, res.Swept, res.Skipped, res.DryRun)
	return nil
}

func runObjectsLog(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root string, out io.Writer) error {
	views, err := objread.New(store, refs, root).List(ctx)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		fmt.Fprintln(out, "no executions")
		return nil
	}
	for _, v := range views {
		marker := ""
		if v.Live {
			marker = " (live)"
		}
		fmt.Fprintf(out, "%-24s %-9s %s rev=%s jobs=%d/%d%s\n",
			v.ExecutionID, v.Status, formatStarted(v.StartedAt), shortID(objectstore.ObjectID(v.RevisionID)),
			v.Summary.JobsSucceeded, v.Summary.JobsTotal, marker)
	}
	return nil
}

// runObjectsShow prints one execution's job/attempt/step detail, from the live
// working tree when in-flight or the sealed object tree otherwise.
func runObjectsShow(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root, ref string, out io.Writer) error {
	v, err := objread.New(store, refs, root).Get(ctx, ref)
	if err != nil {
		return err
	}
	live := ""
	if v.Live {
		live = " (live)"
	}
	fmt.Fprintf(out, "execution %s%s\n", v.ExecutionID, live)
	fmt.Fprintf(out, "  status:   %s\n", v.Status)
	fmt.Fprintf(out, "  revision: %s\n", shortID(objectstore.ObjectID(v.RevisionID)))
	fmt.Fprintf(out, "  started:  %s\n", formatStarted(v.StartedAt))
	if v.FinishedAt != nil {
		fmt.Fprintf(out, "  finished: %s\n", formatStarted(*v.FinishedAt))
	}
	fmt.Fprintf(out, "  jobs:     %d/%d succeeded, %d failed; %d steps\n",
		v.Summary.JobsSucceeded, v.Summary.JobsTotal, v.Summary.JobsFailed, v.Summary.StepsTotal)
	for _, j := range v.Jobs {
		fmt.Fprintf(out, "  • %s [%s]\n", j.JobID, j.Status)
		for _, a := range j.Attempts {
			if len(j.Attempts) > 1 {
				fmt.Fprintf(out, "      attempt %d [%s]\n", a.Attempt, a.Status)
			}
			for _, s := range a.Steps {
				logmark := ""
				if s.HasLog {
					logmark = " (log)"
				}
				fmt.Fprintf(out, "      - %s [%s]%s\n", s.StepID, s.Status, logmark)
			}
		}
	}
	return nil
}

// formatStarted renders a start/finish timestamp for the read commands.
func formatStarted(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func runObjectsReindex(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root string, out io.Writer) error {
	ix := objindex.New(store, refs, root)
	if err := ix.Reindex(ctx); err != nil {
		return err
	}
	entries, err := ix.ListExecutions(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "reindexed %d executions\n", len(entries))
	return nil
}

// openObjectModel opens the object + ref stores rooted at .orun/objectmodel/.
func openObjectModel() (*objectstore.LocalStore, *refstore.LocalRefStore, string, error) {
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve store root: %w", err)
	}
	root := objectModelRoot(abs)
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, nil, "", fmt.Errorf("open object store: %w", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "cli"})
	if err != nil {
		return nil, nil, "", fmt.Errorf("open ref store: %w", err)
	}
	return store, refs, root, nil
}

func runObjectsCat(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, arg string, out io.Writer) error {
	id, err := workingview.ResolveRef(ctx, refs, arg)
	if err != nil {
		return err
	}
	body, err := workingview.CatObject(ctx, store, id)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(body))
	return nil
}

func runObjectsLsTree(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, arg string, out io.Writer) error {
	id, err := workingview.ResolveRef(ctx, refs, arg)
	if err != nil {
		return err
	}
	entries, err := workingview.LsTree(ctx, store, id)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(out, "%-6s %s %s\n", e.Kind, e.Name, e.ID)
	}
	return nil
}

func runObjectsRevParse(ctx context.Context, refs refstore.RefStore, arg string, out io.Writer) error {
	id, err := workingview.ResolveRef(ctx, refs, arg)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(id))
	return nil
}

func runObjectsFsck(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, out io.Writer) error {
	problems, err := workingview.Fsck(ctx, store, refs)
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintln(out, p.String())
	}
	if len(problems) > 0 {
		return fmt.Errorf("fsck: %d problem(s) found", len(problems))
	}
	fmt.Fprintln(out, "ok: object graph healthy")
	return nil
}

func runObjectsCheckout(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root, ref string, out io.Writer) error {
	id, err := workingview.ResolveRef(ctx, refs, ref)
	if err != nil {
		return err
	}
	dest := filepath.Join(root, "current", sanitizeCheckoutName(ref))
	if err := workingview.Materialize(ctx, store, id, dest); err != nil {
		return err
	}
	fmt.Fprintf(out, "checked out %s → %s\n", id, dest)
	return nil
}

// sanitizeCheckoutName folds a ref name into a single safe directory segment.
func sanitizeCheckoutName(ref string) string {
	out := make([]rune, 0, len(ref))
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "object"
	}
	return string(out)
}
