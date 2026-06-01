package main

// read_resolve.go holds the shared resolver helper used by the M5.c
// read-side commands (status, logs, describe, get). Each consumer uses
// it to walk the seven-branch ladder defined by
// internal/executionstate.ResolveExecution and surface the resulting
// (revisionKey, executionKey, legacyExecID) triplet plus the source
// branch that produced it. The helper is read-only.
//
// The legacy ExecID flowing back to callers is the on-disk
// `.orun/executions/<id>` directory name when the resolver matched a
// legacy fallback (compat §4) and otherwise the executionKey itself —
// which the legacy state.Store happens to use as the directory key
// after M5.b.
//
// The helper opens a fresh statestore.LocalStore; callers that already
// have one open should still call this helper (reads are cheap and the
// LocalStore is stateless).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/executionstate"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
)

// resolvedExec is the projected result of an executionstate.ResolveExecution
// call, augmented with the legacy on-disk exec directory name so callers
// can keep using the existing legacy state.Store readers (LoadMetadata,
// LoadState, log walker, etc.).
type resolvedExec struct {
	Source       executionstate.ResolveSource
	Ref          executionstate.ExecutionRef
	RevisionKey  string
	ExecutionKey string
	Store        *state.Store
	// LegacyExecID is the directory name under `.orun/executions/`
	// that holds the runner-on-disk artifacts (state.json,
	// metadata.json, logs/). When the resolver hit
	// ResolveSourceLegacyFallback the value is the original arg;
	// otherwise it falls back to OriginalKey or ExecutionKey so the
	// existing readers can find the on-disk row written by the
	// runner.
	LegacyExecID string
}

// openLocalStateStore opens the workspace's `.orun/` local state store.
// Returns the store plus the absolute path used (so callers can pass
// it to the legacy resolver fallback).
func openLocalStateStore() (statestore.StateStore, string, error) {
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, "", fmt.Errorf("resolve store root: %w", err)
	}
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: abs})
	if err != nil {
		return nil, abs, fmt.Errorf("open state store: %w", err)
	}
	return store, abs, nil
}

// resolveExecutionForRead is the canonical CLI-side wrapper for the
// seven-branch ladder. revHint comes from --revision; arg is the value
// of --exec-id (or a positional run arg) — both may be empty.
func resolveExecutionForRead(ctx context.Context, arg, revHint string) (*resolvedExec, error) {
	store, abs, err := openLocalStateStore()
	if err != nil {
		return nil, err
	}
	ref, err := executionstate.ResolveExecution(ctx, store, arg, revHint, executionstate.ResolveOptions{
		LegacyRoot: executionstate.LegacyRoot(abs),
	})
	if err != nil {
		return nil, err
	}
	legacy := ref.Execution.OriginalKey
	if ref.Source == executionstate.ResolveSourceLegacyFallback && arg != "" {
		legacy = arg
	}
	if legacy == "" {
		legacy = ref.Execution.ExecutionKey
	}
	readStore := state.NewStore(storeDir())
	if !executionArtifactsExist(readStore, legacy) {
		if catStore, catExecID, ok := catalogExecutionReadStore(ctx, store, abs, ref); ok {
			readStore = catStore
			legacy = catExecID
		}
	}
	out := &resolvedExec{
		Source:       ref.Source,
		Ref:          ref,
		RevisionKey:  ref.RevisionKey,
		ExecutionKey: ref.Execution.ExecutionKey,
		Store:        readStore,
		LegacyExecID: legacy,
	}
	// Surface bridge-mirror-failed warnings once per resolved
	// execution. Skipped for legacy-fallback rows since they have no
	// new-layout events directory by definition.
	if ref.Source != executionstate.ResolveSourceLegacyFallback {
		warnBridgeMirrorFailures(ctx, store, ref.RevisionKey, ref.Execution.ExecutionKey)
	}
	return out, nil
}

func executionArtifactsExist(store *state.Store, execID string) bool {
	if store == nil || execID == "" {
		return false
	}
	if fileExistsCheck(store.StatePath(execID)) || fileExistsCheck(store.MetadataPath(execID)) {
		return true
	}
	if info, err := os.Stat(filepath.Join(store.ExecPath(execID), "logs")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func catalogExecutionReadStore(
	ctx context.Context,
	store statestore.StateStore,
	root string,
	ref executionstate.ExecutionRef,
) (*state.Store, string, bool) {
	if ref.RevisionKey == "" || ref.Execution.ExecutionKey == "" {
		return nil, "", false
	}
	revRef, err := revision.ResolveRevision(ctx, store, ref.RevisionKey, revision.ResolveOptions{})
	if err != nil {
		return nil, "", false
	}
	if revRef.Revision.SourceSnapshotKey == "" || revRef.Revision.CatalogSnapshotKey == "" {
		return nil, "", false
	}
	revDir, err := catalogstore.CatalogRevisionDir(
		revRef.Revision.SourceSnapshotKey,
		revRef.Revision.CatalogSnapshotKey,
		ref.RevisionKey,
	)
	if err != nil {
		return nil, "", false
	}
	catStore := &state.Store{BaseDir: filepath.Join(root, filepath.FromSlash(revDir))}
	execID := ref.Execution.ExecutionKey
	if !executionArtifactsExist(catStore, execID) {
		return nil, "", false
	}
	return catStore, execID, true
}
