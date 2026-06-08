package main

// catalog_refs.go implements `orun catalog refs`: the enumeration read path
// that lists every catalog ref (current/main/latest plus branches/<name> and
// prs/<n>) with the source + catalog keys each resolves to. It is pure read —
// no resolution, no writes — over the object-model catalog refs (catalogs/*),
// reading each catalog's source snapshot for its scope/authoritative flag.
// "latest" is surfaced as an alias of "current" (the object model has no
// separate latest ref).

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/objcatalog"
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

	store, refStore, _, err := openObjectModel()
	if err != nil {
		return exitErr(3, "open object model: %w", err)
	}
	reader := objcatalog.New(store, refStore)

	names, lerr := refStore.List(ctx, "catalogs/")
	if lerr != nil {
		// A fresh store with no catalog refs is an empty listing, not an error.
		names = nil
	}
	sort.Strings(names)

	refs := make([]catalogRefEntry, 0, len(names)+1)
	for _, name := range names {
		view, verr := reader.Load(ctx, name)
		if verr != nil {
			continue
		}
		scope, authoritative := objSourceScopeAuth(ctx, store, view.SourceID)
		entry := catalogRefEntry{
			Name:               strings.TrimPrefix(name, "catalogs/"),
			SourceScope:        scope,
			SourceSnapshotKey:  view.SourceID,
			CatalogSnapshotKey: objCatalogSnapshotKey(view),
			Authoritative:      authoritative,
		}
		refs = append(refs, entry)
		// The object model has no separate "latest" ref; surface current's
		// target under that legacy name too for compatibility.
		if entry.Name == "current" {
			latest := entry
			latest.Name = "latest"
			refs = append(refs, latest)
		}
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
