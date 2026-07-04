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
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/spf13/cobra"
)

const kindCatalogDocsResult = "CatalogDocsResult"

// catalogDocsKindFlag mirrors describe's --kind so `docs --kind Repo <name>`
// works; only one catalog command runs per invocation.
var catalogDocsKindFlag string

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
// from an entity/component docs map. It errors (exit 6) when the doc is absent
// or is a bare path pointer with no closure blob (declared but not walked into
// the closure — e.g. a component's docs.overview string).
func docBlobRef(docs map[string]any, kind, key, doc string) (digest, path, sha string, err error) {
	raw, ok := docs[doc]
	if !ok {
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
