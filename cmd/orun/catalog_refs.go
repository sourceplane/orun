package main

// catalog_refs.go implements `orun catalog refs`: the enumeration read path
// that lists every catalog ref (current/main/latest plus branches/<name> and
// prs/<n>) with the source + catalog keys each resolves to. It is pure read
// — no resolution, no writes — and delegates the join of the two ref trees to
// the tested catalogstore.ListRefs seam.

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalogRefEntry is one row of the CatalogRefsResult envelope `data.refs`.
type catalogRefEntry struct {
	Name               string `json:"name"`
	SourceScope        string `json:"sourceScope"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	Authoritative      bool   `json:"authoritative"`
}

// catalogRefsData is the `data` payload of the CatalogRefsResult envelope.
type catalogRefsData struct {
	Refs []catalogRefEntry `json:"refs"`
}

func registerCatalogRefsCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "refs",
		Short: "List every catalog ref with its resolved source/catalog keys",
		Long: `List every catalog ref with the source and catalog snapshot keys it
resolves to.

Refs include the canonical current/main/latest pointers plus any
branches/<name> and prs/<n> refs written by 'orun catalog refresh'. Output is
sorted by ref name. An empty store prints an empty list (exit 0).

Examples:
  orun catalog refs
  orun catalog refs --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogRefs(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogRefs(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	stateStore, _, err := openLocalStateStore()
	if err != nil {
		return exitErr(3, "open state store: %w", err)
	}

	listings, err := catalogstore.ListRefs(ctx, stateStore)
	if err != nil {
		return exitErr(3, "list refs: %w", err)
	}

	refs := make([]catalogRefEntry, 0, len(listings))
	for _, l := range listings {
		refs = append(refs, catalogRefEntry{
			Name:               l.Name,
			SourceScope:        l.SourceScope,
			SourceSnapshotKey:  l.SourceSnapshotKey,
			CatalogSnapshotKey: l.CatalogSnapshotKey,
			Authoritative:      l.Authoritative,
		})
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogRefsResult, catalogRefsData{Refs: refs}, nil)
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	if len(refs) == 0 {
		fmt.Fprintln(os.Stdout, "No catalog refs. Run 'orun catalog refresh' to create one.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "%s\n\n", ui.Bold(color, "Catalog refs"))
	fmt.Fprintf(os.Stdout, "%-22s %-16s %-22s %-22s %s\n",
		"NAME", "SCOPE", "SOURCE", "CATALOG", "AUTH")
	for _, r := range refs {
		auth := ""
		if r.Authoritative {
			auth = "✓"
		}
		fmt.Fprintf(os.Stdout, "%-22s %-16s %-22s %-22s %s\n",
			r.Name, r.SourceScope, r.SourceSnapshotKey, r.CatalogSnapshotKey, auth)
	}
	return nil
}
