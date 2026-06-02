package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objindex"
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
		Use:    "objects",
		Short:  "Inspect the content-addressed object graph",
		Hidden: true,
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
		Short: "List executions newest-first (index-backed, walk fallback)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, refs, root, err := openObjectModel()
			if err != nil {
				return err
			}
			return runObjectsLog(cmd.Context(), store, refs, root, cmd.OutOrStdout())
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

	objectsCmd.AddCommand(catCmd, lsTreeCmd, revParseCmd, fsckCmd, checkoutCmd, logCmd, reindexCmd)
	root.AddCommand(objectsCmd)
}

func runObjectsLog(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, root string, out io.Writer) error {
	entries, err := objindex.New(store, refs, root).ListExecutions(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "no executions")
		return nil
	}
	for _, e := range entries {
		started := e.StartedAt
		if started == "" {
			started = "-"
		}
		fmt.Fprintf(out, "%-24s %-9s %s rev=%s jobs=%d/%d\n",
			e.ExecutionID, e.Status, started, shortID(objectstore.ObjectID(e.RevisionID)),
			e.Summary.JobsSucceeded, e.Summary.JobsTotal)
	}
	return nil
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
