package services

import (
	"context"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
)

// LoadWorkspace discovers the intent root, loads the component tree,
// normalises it, and returns a read-only WorkspaceSnapshot.
//
// It does not shell out and is safe to call from a tea.Cmd goroutine.
func (s *LiveOrunService) LoadWorkspace(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	intentFile := req.IntentFile
	if intentFile == "" {
		intentFile = s.cfg.IntentFile
	}
	intentRoot := s.cfg.IntentRoot

	if intentFile == "" {
		startDir := intentRoot
		if startDir == "" {
			startDir = "."
		}
		found, foundDir, err := discovery.FindIntentFile(startDir)
		if err != nil {
			return nil, err
		}
		intentFile = found
		intentRoot = foundDir
	}

	if intentRoot == "" && intentFile != "" {
		intentRoot = filepath.Dir(intentFile)
	}

	intent, _, err := loader.LoadResolvedIntent(intentFile)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normalised, nerr := normalize.NormalizeIntent(intent)
	if nerr != nil {
		// Normalisation failure should not block the read-only snapshot;
		// surface the error but keep the basic component list usable.
		return nil, nerr
	}

	envNames := environmentNames(intent)
	components := componentSummaries(intent, normalised)

	plans, _ := s.cfg.Store.ListPlans()
	planSummaries := make([]PlanSummary, 0, len(plans))
	for _, p := range plans {
		planSummaries = append(planSummaries, PlanSummary{
			Name:        p.Name,
			Checksum:    p.Checksum,
			JobCount:    p.Jobs,
			GeneratedAt: p.CreatedAt,
		})
	}

	return &WorkspaceSnapshot{
		IntentRoot:   intentRoot,
		IntentName:   intent.Metadata.Name,
		IntentFile:   intentFile,
		Components:   components,
		Environments: envNames,
		Plans:        planSummaries,
		LoadedAt:     time.Now(),
	}, nil
}

func environmentNames(intent *model.Intent) []string {
	if intent == nil {
		return nil
	}
	names := make([]string, 0, len(intent.Environments))
	for name := range intent.Environments {
		names = append(names, name)
	}
	return names
}

func componentSummaries(intent *model.Intent, normalised *model.NormalizedIntent) []ComponentSummary {
	if intent == nil {
		return nil
	}
	out := make([]ComponentSummary, 0, len(intent.Components))
	for _, comp := range intent.Components {
		envs := comp.Subscribe.EnvironmentNames()
		profile := defaultProfile(comp)
		depends := make([]string, 0, len(comp.DependsOn))
		for _, d := range comp.DependsOn {
			depends = append(depends, d.Component)
		}
		out = append(out, ComponentSummary{
			Name:      comp.Name,
			Type:      comp.Type,
			Domain:    comp.Domain,
			Path:      comp.Path,
			Envs:      envs,
			Profile:   profile,
			DependsOn: depends,
		})
	}
	_ = normalised // reserved for richer per-env profile/changed information
	return out
}

func defaultProfile(comp model.Component) string {
	for _, sub := range comp.Subscribe.Environments {
		if sub.Profile != "" {
			return sub.Profile
		}
	}
	return ""
}
