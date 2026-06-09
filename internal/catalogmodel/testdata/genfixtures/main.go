// Command genfixtures bootstraps the canonical golden JSON fixtures under
// internal/catalogmodel/testdata/golden/. Run via `go run` from repo root:
//
//	go run ./internal/catalogmodel/testdata/genfixtures \
//	    ./internal/catalogmodel/testdata/golden
//
// Each fixture is emitted via catalogmodel.PrettyEncode + a trailing newline
// so the roundtrip test (which trims the trailing newline before comparing)
// stays byte-stable while editors keep the file POSIX-clean.
//
// Fixtures intentionally cover every typed field including pointer-to-string
// nullable fields so encode/decode parity is exercised for the full surface.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genfixtures <output-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fixtures := map[string]any{
		"source_snapshot.json":    buildSourceSnapshot(),
		"catalog_snapshot.json":   buildCatalogSnapshot(),
		"component_manifest.json": buildComponentManifest(),
		"catalog_graph.json":      buildCatalogGraph(),
		"component_yaml.json":     buildComponentYAML(),
	}

	for name, v := range fixtures {
		buf, err := catalogmodel.PrettyEncode(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode %s: %v\n", name, err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(buf, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func ptrString(s string) *string { return &s }

func buildSourceSnapshot() catalogmodel.SourceSnapshot {
	return catalogmodel.SourceSnapshot{
		APIVersion:        catalogmodel.APIVersionV1Alpha1,
		Kind:              catalogmodel.KindSourceSnapshot,
		SourceSnapshotKey: "src-branch-main-cdef456a-t5ab21c3",
		SourceSnapshotID:  "src_01JABCDEF0123456789ABCDEF",
		Repo:              "sourceplane/orun",
		RemoteURL:         "git@github.com:sourceplane/orun.git",
		Ref:               "refs/heads/main",
		Branch:            "main",
		SourceScope:       catalogmodel.SourceScopeBranchMain,
		HeadRevision:      "def456a1b2c3",
		TreeHash:          "5ab21c3",
		WorkingTree:       catalogmodel.WorkingTreeClean,
		DirtyHash:         "",
		CatalogInputHash:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:         "2026-05-31T00:00:00Z",
	}
}

func buildCatalogSnapshot() catalogmodel.CatalogSnapshot {
	return catalogmodel.CatalogSnapshot{
		APIVersion:         catalogmodel.APIVersionV1Alpha1,
		Kind:               catalogmodel.KindCatalogSnapshot,
		CatalogSnapshotKey: "cat-c8e91d2a",
		CatalogSnapshotID:  "cat_01JABCDEF0123456789ABCDEF",
		SourceSnapshotKey:  "src-branch-main-cdef456a-t5ab21c3",
		Repo:               "sourceplane/orun",
		SourceScope:        catalogmodel.SourceScopeBranchMain,
		HeadRevision:       "def456a1b2c3",
		TreeHash:           "5ab21c3",
		WorkingTree:        catalogmodel.WorkingTreeClean,
		Authoritative:      true,
		Preview:            false,
		Resolver: catalogmodel.CatalogResolver{
			OrunVersion:     "0.18.0",
			SchemaVersion:   catalogmodel.APIVersionV1Alpha1,
			ResolverVersion: 1,
			StackSources: []string{
				"ghcr.io/sourceplane/stack-tectonic:0.12.0",
			},
		},
		CatalogHash: "sha256:c8e91d2a0000000000000000000000000000000000000000000000000000000",
		Summary: catalogmodel.CatalogSummary{
			Components: 42,
			Systems:    6,
			APIs:       12,
			Resources:  18,
			Owners:     8,
			Domains:    4,
		},
		Objects: catalogmodel.CatalogObjects{
			Components: []catalogmodel.ManifestRef{
				{
					Key:          "sourceplane/orun/api-edge",
					Name:         "api-edge",
					Path:         "components/api-edge/manifest.json",
					ManifestHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				},
			},
		},
		CreatedAt: "2026-05-31T00:00:00Z",
	}
}

func buildComponentManifest() catalogmodel.ComponentManifest {
	return catalogmodel.ComponentManifest{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponentManifest,
		Identity: catalogmodel.ComponentIdentity{
			ComponentID:  "cmp_01JABCDEF0123456789ABCDEF",
			ComponentKey: "sourceplane/orun/api-edge",
			Name:         "api-edge",
			Namespace:    "sourceplane",
			Repo:         "sourceplane/orun",
			Path:         "apps/api-edge",
			SourceFile:   "apps/api-edge/component.yaml",
		},
		Source: catalogmodel.ComponentSource{
			SourceSnapshotKey:  "src-branch-main-cdef456a-t5ab21c3",
			CatalogSnapshotKey: "cat-c8e91d2a",
			Ref:                "refs/heads/main",
			Branch:             "main",
			HeadRevision:       "def456a1b2c3",
			TreeHash:           "5ab21c3",
			WorkingTree:        catalogmodel.WorkingTreeClean,
			ManifestHash:       "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
		Metadata: catalogmodel.ComponentMetadata{
			Title:       "API Edge Worker",
			Description: "Public API gateway for tenant traffic",
			Owner:       "team/platform-edge",
			Maintainers: []string{"team/platform-edge"},
			Contacts: map[string]string{
				"slack": "#platform-edge",
				"email": "platform-edge@example.com",
			},
			Labels: map[string]string{
				"domain":    "edge",
				"tier":      "critical",
				"repo":      "orun",
				"namespace": "sourceplane",
			},
			Tags: []string{"cloudflare", "api", "edge"},
			Annotations: map[string]string{
				"github.com/team":            "platform-edge",
				"datadoghq.com/service-name": "api-edge",
			},
		},
		Spec: catalogmodel.ComponentSpec{
			Type:      "cloudflare-worker",
			Lifecycle: "production",
			System:    "sourceplane-control-plane",
			Domain:    "edge",
			Tier:      "critical",
			Composition: catalogmodel.CompositionRef{
				Source: "ghcr.io/sourceplane/stack-tectonic:0.12.0",
				Type:   "cloudflare-worker",
			},
			Parameters: map[string]string{
				"workerName": "api-edge",
				"stackName":  "api-edge",
			},
			Environments: map[string]catalogmodel.ComponentEnvironment{
				"development": {Profile: "worker.verify", Active: true},
				"staging":     {Profile: "worker.pull_request", Active: true},
				"production":  {Profile: "worker.release", Active: true},
			},
			Dependencies: catalogmodel.ComponentDependencies{
				Components: []catalogmodel.ComponentDependency{
					{
						Key:          "sourceplane/orun/identity-worker",
						Name:         "identity-worker",
						Relationship: catalogmodel.RelCalls,
						Optional:     false,
					},
				},
				APIs: catalogmodel.APIDependencies{
					Provides: []string{"public-api"},
					Consumes: []string{"identity-api"},
				},
				Resources: catalogmodel.ResourceDependencies{
					Uses: []string{"sourceplane/prod/main-postgres"},
				},
			},
		},
		Runtime: catalogmodel.ComponentRuntime{
			Inferred: catalogmodel.ComponentInferred{
				Languages:       []string{"typescript"},
				PackageManagers: []string{"pnpm"},
				Frameworks:      []string{"hono"},
				Infra:           []string{"cloudflare-worker"},
			},
			Files: catalogmodel.ComponentFiles{
				Readme:     ptrString("apps/api-edge/README.md"),
				Package:    ptrString("apps/api-edge/package.json"),
				Dockerfile: nil,
			},
		},
		Resolution: catalogmodel.ComponentResolution{
			InheritedFrom: map[string]string{
				"metadata.labels.repo":                 "intent.yaml:catalog.defaults.labels.repo",
				"metadata.owner":                       "component.yaml:spec.owner",
				"spec.environments.production.profile": "component.yaml:spec.environments.production.profile",
			},
			InferredFrom: map[string][]string{
				"runtime.inferred.languages":  {"apps/api-edge/package.json"},
				"runtime.inferred.frameworks": {"apps/api-edge/package.json"},
			},
		},
	}
}

func buildCatalogGraph() catalogmodel.CatalogGraph {
	return catalogmodel.CatalogGraph{
		APIVersion:         catalogmodel.APIVersionV1Alpha1,
		Kind:               catalogmodel.KindCatalogGraph,
		SourceSnapshotKey:  "src-branch-main-cdef456a-t5ab21c3",
		CatalogSnapshotKey: "cat-c8e91d2a",
		Nodes: []catalogmodel.GraphNode{
			{Key: "sourceplane/orun/api-edge", Kind: catalogmodel.EntityKindComponent, Name: "api-edge"},
			{Key: "sourceplane/orun/identity-worker", Kind: catalogmodel.EntityKindComponent, Name: "identity-worker"},
		},
		Edges: []catalogmodel.GraphEdge{
			{From: "sourceplane/orun/api-edge", To: "sourceplane/orun/identity-worker", Type: catalogmodel.RelCalls, Optional: false},
		},
	}
}

func buildComponentYAML() catalogmodel.ComponentYAML {
	return catalogmodel.ComponentYAML{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponent,
		Metadata: catalogmodel.ComponentYAMLMetadata{
			Name:        "api-edge",
			Title:       "API Edge Worker",
			Description: "Public API gateway for tenant traffic",
			Labels: map[string]string{
				"domain": "edge",
				"tier":   "critical",
			},
			Annotations: map[string]string{
				"github.com/team":            "platform-edge",
				"datadoghq.com/service-name": "api-edge",
			},
		},
		Spec: catalogmodel.ComponentYAMLSpec{
			Type:      "cloudflare-worker",
			Lifecycle: "production",
			Owner:     "team/platform-edge",
			System:    "sourceplane-control-plane",
			Path:      "apps/api-edge",
			DependsOn: []catalogmodel.ComponentYAMLDependency{
				{Component: "identity-worker", Relationship: catalogmodel.RelCalls, Optional: false},
			},
			ProvidesAPIs: []string{"public-api"},
			ConsumesAPIs: []string{"identity-api"},
			Environments: map[string]catalogmodel.ComponentYAMLEnvironment{
				"development": {Profile: "worker.verify"},
				"staging":     {Profile: "worker.pull_request"},
				"production":  {Profile: "worker.release"},
			},
		},
	}
}
