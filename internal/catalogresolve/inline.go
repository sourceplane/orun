package catalogresolve

import (
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// inlineComponent mirrors the change-detection-relevant slice of an inline
// intent.yaml `components:` entry. Only the fields the catalog needs are parsed;
// the rest of the legacy component shape is ignored here.
type inlineComponent struct {
	Name       string             `yaml:"name"`
	Type       string             `yaml:"type"`
	Domain     string             `yaml:"domain"`
	Path       string             `yaml:"path"`
	Owner      string             `yaml:"owner"`
	Lifecycle  string             `yaml:"lifecycle"`
	System     string             `yaml:"system"`
	DependsOn  []inlineDependency `yaml:"dependsOn"`
	Subscribe  *inlineSubscribe   `yaml:"subscribe"`
	Change     *inlineChange      `yaml:"change"`
	Parameters map[string]any     `yaml:"parameters"`
	Labels     map[string]string  `yaml:"labels"`
}

type inlineDependency struct {
	Component    string `yaml:"component"`
	Relationship string `yaml:"relationship"`
	Optional     bool   `yaml:"optional"`
	Include      string `yaml:"include"`
	Input        bool   `yaml:"input"`
}

type inlineSubscribe struct {
	Environments []inlineSubEnv `yaml:"environments"`
}

// inlineSubEnv tolerates both authored forms of a subscribe environment: a bare
// string (`- dev`) and the map form (`- {name: dev, profile: smoke}`). Robust
// parsing here matters because a decode failure would otherwise break the whole
// intent load for every workspace using inline components.
type inlineSubEnv struct {
	Name    string
	Profile string
}

func (e *inlineSubEnv) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		e.Name = node.Value
		return nil
	}
	var m struct {
		Name    string `yaml:"name"`
		Profile string `yaml:"profile"`
	}
	if err := node.Decode(&m); err != nil {
		return err
	}
	e.Name, e.Profile = m.Name, m.Profile
	return nil
}

type inlineChange struct {
	Watches []string `yaml:"watches"`
}

// inlineManifests converts the intent's inline components into AuthoredManifests,
// skipping any whose name collides with an already-discovered component.yaml
// (the discovered file is the richer canonical source and wins). Deterministic:
// returned in lexical name order.
//
// An inline component with a `path` gets a synthetic SourceFile of
// `<path>/component.yaml`, so its component dir resolves for ownership and
// fingerprints; a path-less inline component is still added to the catalog (so
// the component list + dependency graph are complete) but carries no dir, so it
// only participates in change detection via intent/dependency edges.
func inlineManifests(intent *intentFile, discoveredNames map[string]bool) []AuthoredManifest {
	if intent == nil || len(intent.Components) == 0 {
		return nil
	}
	out := make([]AuthoredManifest, 0, len(intent.Components))
	seen := map[string]bool{}
	for _, ic := range intent.Components {
		if ic.Name == "" || discoveredNames[ic.Name] || seen[ic.Name] {
			continue
		}
		seen[ic.Name] = true
		out = append(out, inlineToAuthored(ic))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Component.Metadata.Name < out[j].Component.Metadata.Name
	})
	return out
}

// inlineToAuthored maps one inline component onto the catalogmodel.ComponentYAML
// authoring shape an AuthoredManifest carries.
func inlineToAuthored(ic inlineComponent) AuthoredManifest {
	c := catalogmodel.ComponentYAML{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponent,
		Metadata:   catalogmodel.ComponentYAMLMetadata{Name: ic.Name, Labels: ic.Labels},
		Spec: catalogmodel.ComponentYAMLSpec{
			Type:       ic.Type,
			Domain:     ic.Domain,
			Path:       ic.Path,
			Owner:      ic.Owner,
			Lifecycle:  ic.Lifecycle,
			System:     ic.System,
			Parameters: ic.Parameters,
			Labels:     ic.Labels,
		},
	}
	for _, d := range ic.DependsOn {
		if d.Component == "" {
			continue
		}
		c.Spec.DependsOn = append(c.Spec.DependsOn, catalogmodel.ComponentYAMLDependency{
			Component:    d.Component,
			Relationship: d.Relationship,
			Optional:     d.Optional,
			Include:      d.Include,
			Input:        d.Input,
		})
	}
	if ic.Subscribe != nil && len(ic.Subscribe.Environments) > 0 {
		sub := &catalogmodel.ComponentYAMLSubscribe{}
		for _, e := range ic.Subscribe.Environments {
			if e.Name == "" {
				continue
			}
			sub.Environments = append(sub.Environments, catalogmodel.ComponentYAMLSubscribeEnvironment{Name: e.Name, Profile: e.Profile})
		}
		c.Spec.Subscribe = sub
	}
	if ic.Change != nil && len(ic.Change.Watches) > 0 {
		c.Spec.Change = &catalogmodel.ComponentYAMLChange{Watches: append([]string(nil), ic.Change.Watches...)}
	}

	// Synthetic SourceFile so the component dir resolves; empty when no path.
	source := ""
	if ic.Path != "" {
		source = joinSlash(ic.Path, "component.yaml")
	}
	return AuthoredManifest{SourceFile: source, Component: c}
}
