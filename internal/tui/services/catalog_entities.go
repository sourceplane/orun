package services

// catalog_entities.go serves the Catalog surface: it projects the object-model
// catalog at catalogs/current — components, derived multi-kind entities
// (orun-service-catalog SC3), and the typed relation graph (SC2) — into the
// cockpit's CatalogSnapshot. Unlike freshCatalogComponents this read is NOT
// freshness-gated: the Catalog surface always shows the last resolved catalog
// (the header's "⟳ stale" chip already signals when a refresh would change it).

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// EntityKindOrder is the canonical display order for entity kinds across the
// cockpit (tabs, counts, mixed-kind lists). Kinds absent from a catalog are
// simply skipped.
var EntityKindOrder = []string{
	"Component", "API", "Resource", "System", "Domain",
	"Group", "User", "Composition", "Environment", "Deployment",
}

// LoadCatalog reads catalogs/current into a CatalogSnapshot. Best-effort: an
// absent/empty object model or a missing catalog ref returns (nil, nil) so the
// Catalog surface renders its empty state.
func (s *LiveOrunService) LoadCatalog(ctx context.Context) (*CatalogSnapshot, error) {
	if s.cfg.ObjectModelRoot == "" {
		return nil, nil
	}
	root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, nil
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, nil
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui"})
	if err != nil {
		return nil, nil
	}
	cat, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return nil, nil
	}
	return projectCatalogSnapshot(cat), nil
}

// projectCatalogSnapshot flattens an objcatalog.CatalogView into the cockpit's
// uniform entity projection: components become Kind=Component rows alongside
// the derived entities, sorted by (canonical kind order, name).
func projectCatalogSnapshot(cat objcatalog.CatalogView) *CatalogSnapshot {
	entities := make([]EntitySummary, 0, len(cat.Components)+len(cat.Entities))
	for _, c := range cat.Components {
		entities = append(entities, EntitySummary{
			Kind:        "Component",
			EntityKey:   c.ComponentKey,
			Name:        c.Name,
			Namespace:   c.Namespace,
			Repo:        c.Repo,
			Type:        c.Type,
			Domain:      c.Domain,
			System:      c.System,
			Owner:       c.Owner,
			OwnerSource: c.OwnerSource,
			Stage:       c.Stage,
			Tier:        c.Tier,
			Envs:        sortedEnvNames(c.Environments),
		})
	}
	for _, e := range cat.Entities {
		entities = append(entities, EntitySummary{
			Kind:        e.Kind,
			EntityKey:   e.EntityKey,
			Name:        e.Name,
			Namespace:   e.Namespace,
			Repo:        e.Repo,
			MemberCount: e.MemberCount,
			Members:     append([]string(nil), e.Members...),
			Version:     e.Version,
			Lifecycle:   e.Lifecycle,
		})
	}
	sortEntities(entities)

	counts := make(map[string]int, len(cat.CountsByKind))
	for k, n := range cat.CountsByKind {
		counts[k] = n
	}
	// Older catalogs (pre-SC3) carry no countsByKind; derive from the rows so
	// the tab bar still shows totals.
	if len(counts) == 0 {
		for _, e := range entities {
			counts[e.Kind]++
		}
	}

	relations := make([]RelationSummary, 0, len(cat.Relations))
	for _, r := range cat.Relations {
		relations = append(relations, RelationSummary{
			From:     r.From,
			FromKind: r.FromKind,
			Type:     r.Type,
			To:       r.To,
			ToKind:   r.ToKind,
			Optional: r.Optional,
			Include:  r.Include,
		})
	}

	return &CatalogSnapshot{
		HumanKey:     cat.HumanKey,
		CountsByKind: counts,
		Entities:     entities,
		Relations:    relations,
		LoadedAt:     time.Now(),
	}
}

// kindRank orders kinds canonically; unknown kinds sort after known ones,
// alphabetically.
func kindRank(kind string) int {
	for i, k := range EntityKindOrder {
		if k == kind {
			return i
		}
	}
	return len(EntityKindOrder)
}

func sortEntities(es []EntitySummary) {
	sort.SliceStable(es, func(i, j int) bool {
		ri, rj := kindRank(es[i].Kind), kindRank(es[j].Kind)
		if ri != rj {
			return ri < rj
		}
		if es[i].Kind != es[j].Kind {
			return es[i].Kind < es[j].Kind
		}
		if es[i].Name != es[j].Name {
			return es[i].Name < es[j].Name
		}
		return es[i].EntityKey < es[j].EntityKey
	})
}
