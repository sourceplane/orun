package viewmodel

import (
	"sort"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// CatalogView is the cockpit's view of the resolved catalog — the component
// browse list, optionally annotated with a changed/affected overlay (the Q2
// view). Built from objcatalog.CatalogView (read from the object graph) plus an
// optional affected.Result; the cockpit and any web UI render this, never the
// store directly (specs/orun-catalog-state/consumers.md §2).
type CatalogView struct {
	SourceID   string
	CatalogID  string
	Components []ComponentRow
	// Overlay reports whether a changed/affected overlay was applied (so the
	// renderer can offer the show-only-changed filter).
	Overlay bool
}

// ComponentRow is one row in the catalog browse list.
type ComponentRow struct {
	Key       string
	Name      string
	Type      string
	Domain    string
	Path      string
	Envs      []string // active environment names, sorted
	DependsOn []string

	// Q2 overlay (set when an affected.Result is applied):
	DirectlyChanged bool // its own inputs changed
	Dependent       bool // affected transitively via a dependency
}

// Changed reports whether the row is in the changed/affected set — the predicate
// behind the show-only-changed filter and the row badge.
func (r ComponentRow) Changed() bool { return r.DirectlyChanged || r.Dependent }

// Badge is the short overlay label for the row ("changed", "affected", or "").
func (r ComponentRow) Badge() string {
	switch {
	case r.DirectlyChanged:
		return "changed"
	case r.Dependent:
		return "affected"
	default:
		return ""
	}
}

// EnvBinding is one component-environment binding on the detail page.
type EnvBinding struct {
	Name    string
	Profile string
	Active  bool
}

// ComponentView is the cockpit's component detail page: the resolved manifest
// fields plus (later) a Jobs section from the component→executions join.
type ComponentView struct {
	Key       string
	Name      string
	Namespace string
	Repo      string
	Type      string
	Domain    string
	Path      string
	Envs      []EnvBinding
	DependsOn []string
	Watches   []string
	Metadata  map[string]any
	Spec      map[string]any
}

// BuildCatalogView maps an objcatalog.CatalogView into the cockpit catalog view.
// When overlay is non-nil, each row is annotated with its changed/affected state
// from the affected.Result (matched by component key).
func BuildCatalogView(cat objcatalog.CatalogView, overlay *affected.Result) CatalogView {
	v := CatalogView{
		SourceID:  cat.SourceID,
		CatalogID: string(cat.ObjectID),
		Overlay:   overlay != nil,
	}

	var changed, dependent map[string]bool
	if overlay != nil {
		changed = toSet(overlay.DirectlyChanged)
		dependent = toSet(overlay.Dependents)
	}

	v.Components = make([]ComponentRow, 0, len(cat.Components))
	for _, c := range cat.Components {
		row := ComponentRow{
			Key:       c.ComponentKey,
			Name:      c.Name,
			Type:      c.Type,
			Domain:    c.Domain,
			Path:      c.Path,
			Envs:      activeEnvNames(c.Environments),
			DependsOn: append([]string(nil), c.DependsOn...),
		}
		if overlay != nil {
			row.DirectlyChanged = changed[c.ComponentKey]
			row.Dependent = !row.DirectlyChanged && dependent[c.ComponentKey]
		}
		v.Components = append(v.Components, row)
	}
	return v
}

// FilterChanged returns a copy of the view containing only changed/affected
// rows (the show-only-changed filter). With no overlay it returns the view
// unchanged.
func (v CatalogView) FilterChanged() CatalogView {
	if !v.Overlay {
		return v
	}
	out := v
	out.Components = make([]ComponentRow, 0, len(v.Components))
	for _, r := range v.Components {
		if r.Changed() {
			out.Components = append(out.Components, r)
		}
	}
	return out
}

// BuildComponentView maps one resolved manifest view into the detail page.
func BuildComponentView(c objcatalog.CatalogComponentView) ComponentView {
	cv := ComponentView{
		Key:       c.ComponentKey,
		Name:      c.Name,
		Namespace: c.Namespace,
		Repo:      c.Repo,
		Type:      c.Type,
		Domain:    c.Domain,
		Path:      c.Path,
		DependsOn: append([]string(nil), c.DependsOn...),
		Watches:   specWatches(c.Spec),
		Metadata:  c.Metadata,
		Spec:      c.Spec,
	}
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := c.Environments[name]
		cv.Envs = append(cv.Envs, EnvBinding{Name: name, Profile: e.Profile, Active: e.Active})
	}
	return cv
}

// --- internals --------------------------------------------------------

func toSet(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// activeEnvNames returns the sorted names of the environments the component is
// active in.
func activeEnvNames(envs map[string]objcatalog.EnvView) []string {
	if len(envs) == 0 {
		return nil
	}
	out := make([]string, 0, len(envs))
	for name, e := range envs {
		if e.Active {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// specWatches reads spec.change.watches from the verbatim manifest spec.
func specWatches(spec map[string]any) []string {
	change, ok := spec["change"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := change["watches"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, w := range raw {
		if s, ok := w.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
