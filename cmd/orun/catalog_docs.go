package main

// catalog_docs.go implements `orun catalog docs <entity> [doc]` (WO3.1c): render
// the bytes of a resolved doc that was walked into the catalog closure as a
// content-addressed blob. The Repo entity's `docs.overview` is the canonical
// case — this is the local preview of the exact bytes orun-cloud renders at the
// read edge (same digest, no git round-trip).
//
// A doc reference must be a doc_ref object {path,sha,digest}; a bare path string
// (declared but not walked into the closure) has no content and exits 6.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/spf13/cobra"
)

const kindCatalogDocsResult = "CatalogDocsResult"

// catalogDocsKindFlag mirrors describe's --kind so `docs --kind Repo <name>`
// works; only one catalog command runs per invocation.
var catalogDocsKindFlag string

// catalogDocsListFlag switches to the shelf listing (CD1).
var catalogDocsListFlag bool

// catalogDocsData is the --json payload for `catalog docs`.
type catalogDocsData struct {
	Entity  string `json:"entity"`
	Kind    string `json:"kind"`
	Doc     string `json:"doc"`
	Path    string `json:"path,omitempty"`
	Sha     string `json:"sha,omitempty"`
	Digest  string `json:"digest"`
	Content string `json:"content"`
}

// catalogDocsListData is the --json payload for `catalog docs --list` (CD1):
// the entity's full doc set — the shelf.
type catalogDocsListData struct {
	Entity string               `json:"entity"`
	Kind   string               `json:"kind"`
	Docs   []catalogDocsListRow `json:"docs"`
}

type catalogDocsListRow struct {
	Key    string `json:"key"`
	Title  string `json:"title,omitempty"`
	Role   string `json:"role,omitempty"`
	Path   string `json:"path,omitempty"`
	Commit string `json:"commit,omitempty"`
	Digest string `json:"digest,omitempty"`
	Size   int    `json:"size,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func registerCatalogDocsCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "docs <entity> [doc]",
		Short: "Print a resolved doc (default: overview) carried in the catalog closure",
		Long: `Print the bytes of a resolved doc for one entity.

The doc must have been walked into the catalog closure as a content-addressed
blob (a doc_ref {path,sha,digest}); the Repo kind's docs.overview is the
canonical case. This is the local preview of exactly what orun-cloud renders at
the read edge — the same content address, no git round-trip.

The entity is addressed the same way as 'catalog describe':
  - <kind>:<key>              e.g. repo:sourceplane/ogpic/ogpic
  - --kind <Kind> <name|key>
  - bare kind keyword         e.g. repo (the one Repo entity)
  - a Component name/key
The optional [doc] selects which doc (default: overview).

Examples:
  orun catalog docs repo:sourceplane/ogpic/ogpic
  orun catalog docs repo
  orun catalog docs repo:sourceplane/ogpic/ogpic --json

Exit codes:
  0  Doc found and printed.
  1  Invalid selector or missing argument.
  3  StateStore failure.
  4  Ambiguous name across repos/kinds.
  6  Catalog, entity, or doc (with content) not found.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := "overview"
			if len(args) == 2 {
				doc = args[1]
			}
			return runCatalogDocs(cmd.Context(), args[0], doc)
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().StringVar(&catalogDocsKindFlag, "kind", "", "Address an entity of this kind instead of a Component")
	cmd.Flags().BoolVar(&catalogDocsListFlag, "list", false, "List the entity's full doc set (the shelf) instead of printing one doc")
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output (includes the doc content)")

	parent.AddCommand(cmd)
}

func runCatalogDocs(ctx context.Context, arg, doc string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return exitErr(1, "docs requires an entity")
	}
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return exitErr(1, "docs requires a non-empty doc name")
	}

	ref, err := objCatalogRef(catalogSourceFlag, catalogSnapshotFlag)
	if err != nil {
		return err
	}
	store, refs, _, err := openObjectModel()
	if err != nil {
		return exitErr(3, "open object model: %w", err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, ref)
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return exitErr(6, "resolve catalog: not found (run 'orun catalog refresh' first): %w", err)
		}
		return exitErr(3, "resolve catalog: %w", err)
	}

	// Resolve the docs map + identity via the same selector grammar as describe.
	var docs map[string]any
	var kind, key string
	if k, ky, isEntity := parseEntitySelector(arg, catalogDocsKindFlag); isEntity && !(ky == "" && componentNamed(view, arg)) {
		e, serr := selectObjEntity(view, k, ky)
		if serr != nil {
			return serr
		}
		docs, kind, key = e.Docs, e.Kind, e.EntityKey
	} else {
		c, serr := selectObjComponent(view, arg)
		if serr != nil {
			return serr
		}
		docs, kind, key = c.Docs, "Component", c.ComponentKey
	}

	if catalogDocsListFlag {
		return writeCatalogDocsList(kind, key, docs)
	}

	digest, path, sha, err := docBlobRef(docs, kind, key, doc)
	if err != nil {
		return err
	}
	_, body, err := store.Get(ctx, objectstore.ObjectID(digest))
	if err != nil {
		return exitErr(6, "read doc blob %s: %w", digest, err)
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogDocsResult, catalogDocsData{
			Entity:  key,
			Kind:    kind,
			Doc:     doc,
			Path:    path,
			Sha:     sha,
			Digest:  digest,
			Content: string(body),
		}, nil)
	}
	_, err = os.Stdout.Write(body)
	return err
}

// docBlobRef extracts the content-addressed digest (+ path/sha) of a named doc
// from an entity/component docs map. The doc is addressed by key: "overview"
// (or any top-level ref) first, then the docs.pages[] entry with that key
// (saas-catalog-docs CD1). It errors (exit 6) when the doc is absent or is a
// declared-only pointer with no closure blob.
func docBlobRef(docs map[string]any, kind, key, doc string) (digest, path, sha string, err error) {
	raw, ok := docs[doc]
	if !ok {
		if pm := docPageByKey(docs, doc); pm != nil {
			digest = anyString(pm["digest"])
			if digest == "" {
				reason := anyString(pm["reason"])
				if reason == "" {
					reason = "not walked into the closure"
				}
				return "", "", "", exitErr(6, "%s %q doc %q has no content (%s)", kind, key, doc, reason)
			}
			return digest, anyString(pm["path"]), "", nil
		}
		return "", "", "", exitErr(6, "%s %q has no %q doc", kind, key, doc)
	}
	dm, ok := raw.(map[string]any)
	if !ok {
		return "", "", "", exitErr(6, "%s %q doc %q is a path pointer only (%v), not walked into the closure — no content to print", kind, key, doc, raw)
	}
	digest = anyString(dm["digest"])
	if digest == "" {
		return "", "", "", exitErr(6, "%s %q doc %q has no digest (not walked into the closure)", kind, key, doc)
	}
	return digest, anyString(dm["path"]), anyString(dm["sha"]), nil
}

// docPageByKey finds the docs.pages[] entry with the given key, or nil.
func docPageByKey(docs map[string]any, key string) map[string]any {
	pages, _ := docs["pages"].([]any)
	for _, raw := range pages {
		pm, _ := raw.(map[string]any)
		if pm == nil {
			continue
		}
		if k := anyString(pm["key"]); k == key {
			return pm
		}
	}
	return nil
}

// writeCatalogDocsList prints the entity's doc set — the shelf (CD1): the
// overview first, then pages in declared order, each with its attachment
// state (attached@commit / declared-only + reason).
func writeCatalogDocsList(kind, key string, docs map[string]any) error {
	rows := docShelfRows(docs)
	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogDocsResult, catalogDocsListData{Entity: key, Kind: kind, Docs: rows}, nil)
	}
	if len(rows) == 0 {
		fmt.Fprintf(os.Stdout, "%s %s declares no docs\n", kind, key)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tTITLE\tROLE\tPATH\tSTATE")
	for _, r := range rows {
		state := "declared-only"
		if r.Reason != "" {
			state = "declared-only (" + r.Reason + ")"
		}
		if r.Digest != "" {
			state = "attached"
			if r.Commit != "" {
				c := r.Commit
				if len(c) > 12 {
					c = c[:12]
				}
				state = "attached@" + c
			}
		}
		title := r.Title
		if r.Key == "overview" && title == "" {
			title = "Overview"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Key, title, r.Role, r.Path, state)
	}
	return w.Flush()
}

// docShelfRows normalizes a wire docs map into shelf rows: overview first
// (whether a ref object or a bare path pointer), then pages in order.
func docShelfRows(docs map[string]any) []catalogDocsListRow {
	var rows []catalogDocsListRow
	switch ov := docs["overview"].(type) {
	case map[string]any:
		rows = append(rows, catalogDocsListRow{
			Key: "overview", Path: anyString(ov["path"]),
			Commit: anyString(ov["commit"]), Digest: anyString(ov["digest"]),
		})
	case string:
		if ov != "" {
			rows = append(rows, catalogDocsListRow{Key: "overview", Path: ov})
		}
	}
	pages, _ := docs["pages"].([]any)
	for _, raw := range pages {
		pm, _ := raw.(map[string]any)
		if pm == nil {
			continue
		}
		row := catalogDocsListRow{
			Key: anyString(pm["key"]), Title: anyString(pm["title"]), Role: anyString(pm["role"]),
			Path: anyString(pm["path"]), Commit: anyString(pm["commit"]),
			Digest: anyString(pm["digest"]), Reason: anyString(pm["reason"]),
		}
		if n, ok := pm["size"].(float64); ok {
			row.Size = int(n)
		}
		rows = append(rows, row)
	}
	return rows
}
