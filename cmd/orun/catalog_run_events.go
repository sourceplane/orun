package main

// catalog_run_events.go — C7 event orchestrator for the orun run / catalog
// integration. Emits execution.started / execution.completed /
// execution.failed component history events and updates the catalog-local
// component execution index so `orun catalog history <component>` shows
// run executions.
//
// Both entry points are best-effort: errors are logged to stderr but
// MUST NOT fail the run.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/ui"
)

// emitCatalogExecutionStarted is called after CreateExecution succeeds.
// Emits execution.started for each unique (component, environment) in
// the plan.
func emitCatalogExecutionStarted(
	ctx context.Context,
	rx *revisionExecution,
	plan *model.Plan,
	store statestore.StateStore,
) {
	if rx == nil || !rx.catalogParent.Active() || plan == nil {
		return
	}

	repo := readSourceRepo(ctx, store, rx.catalogParent.SourceKey)
	now := catalogRunEventTime(rx)

	catStore := catalogstore.New(store)
	seen := map[string]struct{}{}

	for _, job := range plan.Jobs {
		if job.Component == "" {
			continue
		}
		dedup := job.Component + "/" + job.Environment
		if _, ok := seen[dedup]; ok {
			continue
		}
		seen[dedup] = struct{}{}

		compKey := componentKeyForJob(ctx, store, rx, repo, job.Component)
		ev := catalogmodel.ComponentHistoryEvent{
			APIVersion:         catalogmodel.APIVersionV1Alpha1,
			Kind:               catalogmodel.KindComponentEvent,
			EventType:          catalogmodel.EventExecutionStarted,
			ComponentKey:       compKey,
			SourceSnapshotKey:  rx.catalogParent.SourceKey,
			CatalogSnapshotKey: rx.catalogParent.CatalogKey,
			RevisionKey:        rx.revKey,
			ExecutionKey:       rx.execKey,
			TriggerName:        rx.triggerName,
			Profile:            job.Profile,
			Environment:        job.Environment,
			Status:             "started",
			At:                 now,
		}
		if err := catStore.AppendComponentEvent(ctx, ev); err != nil {
			warnCatalogEvent("execution.started", job.Component, err)
		}
	}
}

// emitCatalogExecutionTerminal is called from finalizeRevisionExecution
// after MarkTerminal succeeds. Emits execution.completed or
// execution.failed for each unique (component, environment) in the plan,
// and updates the component execution index.
func emitCatalogExecutionTerminal(
	ctx context.Context,
	rx *revisionExecution,
	plan *model.Plan,
	store statestore.StateStore,
	status string,
) {
	if rx == nil || !rx.catalogParent.Active() || plan == nil {
		return
	}

	repo := readSourceRepo(ctx, store, rx.catalogParent.SourceKey)
	now := catalogRunEventTime(rx)

	eventType := catalogmodel.EventExecutionCompleted
	if status == "failed" {
		eventType = catalogmodel.EventExecutionFailed
	}

	catStore := catalogstore.New(store)
	seen := map[string]struct{}{}

	for _, job := range plan.Jobs {
		if job.Component == "" {
			continue
		}
		dedup := job.Component + "/" + job.Environment
		if _, ok := seen[dedup]; ok {
			continue
		}
		seen[dedup] = struct{}{}

		compKey := componentKeyForJob(ctx, store, rx, repo, job.Component)
		ev := catalogmodel.ComponentHistoryEvent{
			APIVersion:         catalogmodel.APIVersionV1Alpha1,
			Kind:               catalogmodel.KindComponentEvent,
			EventType:          eventType,
			ComponentKey:       compKey,
			SourceSnapshotKey:  rx.catalogParent.SourceKey,
			CatalogSnapshotKey: rx.catalogParent.CatalogKey,
			RevisionKey:        rx.revKey,
			ExecutionKey:       rx.execKey,
			TriggerName:        rx.triggerName,
			Profile:            job.Profile,
			Environment:        job.Environment,
			Status:             status,
			At:                 now,
		}
		if err := catStore.AppendComponentEvent(ctx, ev); err != nil {
			warnCatalogEvent(eventType, job.Component, err)
		}

		row := catalogmodel.ComponentExecutionRow{
			RevisionKey:  rx.revKey,
			ExecutionKey: rx.execKey,
			TriggerName:  rx.triggerName,
			Profile:      job.Profile,
			Environment:  job.Environment,
			Status:       status,
			CreatedAt:    now,
		}
		if err := catalogstore.WriteComponentExecutionIndex(
			ctx, store,
			rx.catalogParent.SourceKey, rx.catalogParent.CatalogKey,
			job.Component, row,
		); err != nil {
			warnCatalogEvent("execution-index-update", job.Component, err)
		}
	}
}

func catalogRunEventTime(rx *revisionExecution) string {
	if rx != nil && rx.cfg.Now != nil {
		return rx.cfg.Now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func componentKeyForJob(ctx context.Context, store statestore.StateStore, rx *revisionExecution, repo, component string) string {
	if rx != nil && rx.catalogParent.Active() {
		if manifest, err := catalogstore.ReadComponentManifest(ctx, store, rx.catalogParent.SourceKey, rx.catalogParent.CatalogKey, component); err == nil &&
			strings.TrimSpace(manifest.Identity.ComponentKey) != "" {
			return manifest.Identity.ComponentKey
		}
	}
	return buildComponentKey(repo, component)
}

// readSourceRepo reads the source.json from the catalog store and
// extracts the Repo field. Returns "" on any failure so callers can
// fall back gracefully.
func readSourceRepo(ctx context.Context, store statestore.StateStore, srcKey string) string {
	docPath, err := catalogstore.SourceDocPath(srcKey)
	if err != nil {
		return ""
	}
	raw, _, err := store.Read(ctx, docPath)
	if err != nil {
		return ""
	}
	var src struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return ""
	}
	return src.Repo
}

// buildComponentKey constructs a 3-segment component key from the
// repo name and component name. If repo is missing, uses "_/_/<name>".
// If repo contains an org/repo pair (e.g. "sourceplane/orun"), uses
// that directly: "<org>/<repo>/<name>".
func buildComponentKey(repo, component string) string {
	if repo == "" {
		return catalogmodel.FormatComponentKey("_", "_", component)
	}
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return catalogmodel.FormatComponentKey(parts[0], parts[1], component)
	}
	return catalogmodel.FormatComponentKey("_", repo, component)
}

func warnCatalogEvent(kind, component string, err error) {
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s catalog event %s for %s: %v\n",
		ui.Yellow(color, "warning:"), kind, component, err)
}
