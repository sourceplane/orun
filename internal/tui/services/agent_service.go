package services

import (
	"context"
	"sort"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
)

// AgentTypeRow is the cockpit projection of one AgentType catalog entity
// (orun-agents AG1): a git-authored agents/*.md sealed into the catalog. It is
// the data the TUI Agent mode's type list renders — read from the same
// content-addressed catalog every other cockpit surface reads.
type AgentTypeRow struct {
	Name      string
	Harness   string
	Model     string
	Owner     string
	Autonomy  string
	Extends   string
	MayAffect []string // resolved component keys (from typed mayAffect edges)
	Persona   string   // the persona doc body, when materialized
}

// LoadAgentTypes reads the AgentType entities from catalogs/current. Best-effort
// (an absent object model or catalog returns nil), matching LoadCatalog.
func (s *LiveOrunService) LoadAgentTypes(ctx context.Context) ([]AgentTypeRow, error) {
	store, refs, ok := openObjectModel(s.cfg.ObjectModelRoot)
	if !ok {
		return nil, nil
	}
	cat, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return nil, nil
	}
	var rows []AgentTypeRow
	for _, e := range cat.Entities {
		if e.Kind != "AgentType" {
			continue
		}
		row := AgentTypeRow{Name: e.Name, Owner: e.Owner}
		if e.Metadata != nil {
			row.Harness, _ = e.Metadata["harness"].(string)
			row.Model, _ = e.Metadata["model"].(string)
			row.Autonomy, _ = e.Metadata["autonomyDefault"].(string)
			row.Extends, _ = e.Metadata["extends"].(string)
			row.MayAffect = anySliceToStrings(e.Metadata["mayAffect"])
		}
		row.Persona = readEntityDoc(ctx, store, e, "persona")
		sort.Strings(row.MayAffect)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// anySliceToStrings coerces a projected metadata list ([]any of strings) to
// []string, tolerating a plain []string too.
func anySliceToStrings(v any) []string {
	switch xs := v.(type) {
	case []string:
		return append([]string(nil), xs...)
	case []any:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// readEntityDoc resolves an entity's doc-key body by its content digest,
// returning "" when absent or unreadable.
func readEntityDoc(ctx context.Context, store *objectstore.LocalStore, e objcatalog.EntityView, key string) string {
	doc, _ := e.Docs[key].(map[string]any)
	if doc == nil {
		return ""
	}
	digest, _ := doc["digest"].(string)
	if digest == "" {
		return ""
	}
	_, body, err := store.Get(ctx, objectstore.ObjectID(digest))
	if err != nil {
		return ""
	}
	return string(body)
}
