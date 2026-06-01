package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/executionstate"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
)

func TestEmitCatalogExecutionEventsAndIndex(t *testing.T) {
	dir := withTempIntentRoot(t)
	ctx := context.Background()
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	parent := revision.CatalogParentRef{
		SourceKey:  "src-branch-main-abcdef0",
		CatalogKey: "cat-abcdef",
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	rx := &revisionExecution{
		cfg: executionstate.Config{
			Store:         store,
			Now:           func() time.Time { return now },
			CatalogParent: parent,
		},
		revKey:        "rev-main-abcdef0-pfeedface",
		execKey:       "gh-123456-1-feedface",
		exec:          executionstate.ExecutionRun{TriggerKey: "trg-main-abcdef0"},
		catalogParent: parent,
		triggerName:   "system.manual",
	}
	plan := &model.Plan{Jobs: []model.PlanJob{{
		ID:          "api-edge.dev.echo",
		Component:   "api-edge",
		Environment: "dev",
		Profile:     "worker.verify",
	}}}

	srcPath, err := catalogstore.SourceDocPath(parent.SourceKey)
	if err != nil {
		t.Fatalf("SourceDocPath: %v", err)
	}
	if _, err := store.Write(ctx, srcPath, []byte(`{"repo":"sourceplane/orun"}`+"\n"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("write source doc: %v", err)
	}
	manifestPath, err := catalogstore.ComponentManifestPath(parent.SourceKey, parent.CatalogKey, "api-edge")
	if err != nil {
		t.Fatalf("ComponentManifestPath: %v", err)
	}
	manifest := catalogmodel.ComponentManifest{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponentManifest,
		Identity: catalogmodel.ComponentIdentity{
			ComponentKey: "sourceplane/orun/api-edge",
			Name:         "api-edge",
		},
	}
	manifestBody, err := catalogmodel.PrettyEncode(manifest)
	if err != nil {
		t.Fatalf("PrettyEncode manifest: %v", err)
	}
	if _, err := store.Write(ctx, manifestPath, manifestBody, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	emitCatalogExecutionStarted(ctx, rx, plan, store)
	emitCatalogExecutionTerminal(ctx, rx, plan, store, executionstate.StatusCompleted)

	eventsDir := "sources/" + parent.SourceKey + "/catalogs/" + parent.CatalogKey + "/history/components/api-edge/events"
	infos, err := store.List(ctx, eventsDir)
	if err != nil {
		t.Fatalf("List events: %v", err)
	}
	seen := map[string]bool{}
	for _, info := range infos {
		if filepath.Base(info.Path) == "seq.lock" {
			continue
		}
		raw, _, err := store.Read(ctx, info.Path)
		if err != nil {
			t.Fatalf("read event %s: %v", info.Path, err)
		}
		var ev catalogmodel.ComponentHistoryEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("decode event %s: %v", info.Path, err)
		}
		seen[ev.EventType] = true
		if ev.ComponentKey != "sourceplane/orun/api-edge" {
			t.Fatalf("ComponentKey = %q", ev.ComponentKey)
		}
		if ev.ExecutionKey != rx.execKey || ev.RevisionKey != rx.revKey {
			t.Fatalf("event lineage mismatch: %+v", ev)
		}
	}
	for _, kind := range []string{catalogmodel.EventExecutionStarted, catalogmodel.EventExecutionCompleted} {
		if !seen[kind] {
			t.Fatalf("missing event %s in %v", kind, seen)
		}
	}

	idx, found, err := catalogstore.ReadComponentExecutionIndex(ctx, store, parent.SourceKey, parent.CatalogKey, "api-edge")
	if err != nil {
		t.Fatalf("ReadComponentExecutionIndex: %v", err)
	}
	if !found || len(idx.Executions) != 1 {
		t.Fatalf("index found=%v executions=%v", found, idx.Executions)
	}
	row := idx.Executions[0]
	if row.ExecutionKey != rx.execKey || row.Status != executionstate.StatusCompleted ||
		row.TriggerName != "system.manual" || row.Profile != "worker.verify" || row.Environment != "dev" {
		t.Fatalf("bad execution row: %+v", row)
	}
}

func TestEmitCatalogExecutionFailedEvent(t *testing.T) {
	dir := withTempIntentRoot(t)
	ctx := context.Background()
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	parent := revision.CatalogParentRef{SourceKey: "src-branch-main-abcdef0", CatalogKey: "cat-abcdef"}
	rx := &revisionExecution{
		cfg:           executionstate.Config{Store: store, CatalogParent: parent},
		revKey:        "rev-main-abcdef0-pfeedface",
		execKey:       "run-failed",
		exec:          executionstate.ExecutionRun{TriggerKey: "trg-main-abcdef0"},
		catalogParent: parent,
		triggerName:   "system.manual",
	}
	plan := &model.Plan{Jobs: []model.PlanJob{{Component: "api-edge", Environment: "dev"}}}

	emitCatalogExecutionTerminal(ctx, rx, plan, store, executionstate.StatusFailed)

	eventsDir := "sources/" + parent.SourceKey + "/catalogs/" + parent.CatalogKey + "/history/components/api-edge/events"
	infos, err := store.List(ctx, eventsDir)
	if err != nil {
		t.Fatalf("List events: %v", err)
	}
	for _, info := range infos {
		if filepath.Base(info.Path) == "seq.lock" {
			continue
		}
		raw, _, err := store.Read(ctx, info.Path)
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		var ev catalogmodel.ComponentHistoryEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.EventType == catalogmodel.EventExecutionFailed {
			return
		}
	}
	t.Fatal("missing execution.failed event")
}
