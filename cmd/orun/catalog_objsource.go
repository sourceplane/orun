package main

// catalog_objsource.go is the shared object-model read seam for the
// `orun catalog *` read commands as they migrate off internal/catalogstore
// (specs/orun-legacy-retirement Bucket 1). It maps the shared
// --catalog-source/--catalog-snapshot selector grammar onto object-model
// catalog refs, opens the object store, and adapts the objcatalog read view
// back to the catalogmodel graph types the renderers already use.

import (
	"context"
	"errors"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objread"
)

// objCatalogRef maps the legacy selector grammar
// (current|main|latest|branches/<name>|prs/<n>|<id>) and the explicit
// --catalog-snapshot pin onto an object-model catalog ref name (or a bare
// catalog object id), the argument objcatalog.Load accepts.
//
// The legacy `cat-<key>` human-key form is not an object-model ref; an
// explicit pin is therefore expected to be a catalog object id or a ref path,
// passed verbatim to Load (which resolves it or returns ErrNotFound → exit 6).
func objCatalogRef(source, snapshot string) (string, error) {
	if s := strings.TrimSpace(snapshot); s != "" {
		// Explicit pin: a catalog object id or ref path, used verbatim.
		return s, nil
	}
	s := strings.TrimSpace(source)
	switch s {
	case "", "current", "latest":
		return "catalogs/current", nil
	case "main":
		return "catalogs/main", nil
	}
	if rest, ok := strings.CutPrefix(s, "branches/"); ok && rest != "" {
		return "catalogs/branches/" + rest, nil
	}
	if rest, ok := strings.CutPrefix(s, "prs/"); ok && rest != "" {
		return "catalogs/prs/" + rest, nil
	}
	if isObjCatalogID(s) {
		return s, nil
	}
	return "", exitErr(1, "invalid catalog selector %q", source)
}

// isObjCatalogID reports whether s looks like an "<algo>:<hex>" content id
// (the same shape objcatalog.Load treats as a bare id rather than a ref name).
func isObjCatalogID(s string) bool {
	i := strings.IndexByte(s, ':')
	if i <= 0 || strings.Contains(s, "/") {
		return false
	}
	hex := s[i+1:]
	if hex == "" {
		return false
	}
	for _, c := range hex {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// loadObjCatalog resolves the shared selector flags to an object-model catalog
// ref, opens the object store once, and returns both the loaded catalog view
// and an objread.Reader sharing the same stores (so a command can join the
// catalog against execution history without re-opening). A missing catalog is
// mapped to the exit-6 "run refresh" contract; any other read failure is exit 3.
func loadObjCatalog(ctx context.Context) (objcatalog.CatalogView, *objread.Reader, error) {
	ref, err := objCatalogRef(catalogSourceFlag, catalogSnapshotFlag)
	if err != nil {
		return objcatalog.CatalogView{}, nil, err
	}
	store, refs, root, err := openObjectModel()
	if err != nil {
		return objcatalog.CatalogView{}, nil, exitErr(3, "open object model: %w", err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, ref)
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return objcatalog.CatalogView{}, nil, exitErr(6, "resolve catalog: not found (run 'orun catalog refresh' first): %w", err)
		}
		return objcatalog.CatalogView{}, nil, exitErr(3, "resolve catalog: %w", err)
	}
	return view, objread.New(store, refs, root), nil
}

// mapString reads a string field from a verbatim metadata/spec map carried on
// an objcatalog component view (e.g. metadata.owner, spec.system).
func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// objCatalogSnapshotKey is the user-facing catalogSnapshotKey for an
// object-model catalog view: the human key when present, else the catalog
// object id. (Replaces the legacy cat-<key>; no test pins the literal value.)
func objCatalogSnapshotKey(view objcatalog.CatalogView) string {
	if view.HumanKey != "" {
		return view.HumanKey
	}
	return string(view.ObjectID)
}

// objGraphToCatalogModel adapts one objcatalog graph slice to the
// catalogmodel.CatalogGraph the existing renderers consume. Only Nodes/Edges
// are read by the renderers; the field mapping is one-to-one (Optional now
// round-trips through the object model).
func objGraphToCatalogModel(gv objcatalog.GraphView) catalogmodel.CatalogGraph {
	out := catalogmodel.CatalogGraph{Kind: catalogmodel.KindCatalogGraph}
	for _, n := range gv.Nodes {
		out.Nodes = append(out.Nodes, catalogmodel.GraphNode{Key: n.Key, Kind: n.Kind, Name: n.Name})
	}
	for _, e := range gv.Edges {
		out.Edges = append(out.Edges, catalogmodel.GraphEdge{From: e.From, To: e.To, Type: e.Type, Optional: e.Optional, Include: e.Include})
	}
	return out
}
