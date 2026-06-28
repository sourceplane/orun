package main

// catalog_autosync.go is the config-driven, best-effort half of catalog
// publishing (the explicit half is `plan --push-catalog` / `catalog refresh
// --push`). When intent.yaml sets `execution.state.autopushCatalog: true`, a
// successful `orun plan` publishes the resolved catalog to the configured
// backend — but only under tight guardrails, and never at the cost of failing
// the plan:
//
//   - Default branch, clean tree only. Gated on the source scope being
//     branch-main, so a feature branch or an uncommitted edit never moves the
//     project-wide head. (A dirty tree resolves to local-dirty, so it is
//     excluded by the same gate.)
//   - Debounced by catalog digest. A local marker records the last
//     auto-published catalog id; an unchanged catalog is a no-op (no network),
//     so repeated plans on the same commit cost nothing.
//   - Best-effort. Logged-out / unlinked / unreachable backends are swallowed
//     (surfaced only under ORUN_VERBOSE); the plan's exit code is never touched
//     (design.md §7: catalog push failure → warn, exit 0). A *successful* push
//     still prints pushResolvedCatalog's banner, because publishing is a
//     team-visible act (design §5 "never silent").
//
// This is deliberately wired into `plan` only — the deliberate, catalog-
// resolving command teams run constantly — not the universal refresh hook.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// autoPushCatalogTimeout bounds the best-effort publish so an unreachable
// backend never stalls the plan it rides on. It covers the whole publish — the
// OIDC exchange, the object-closure upload, and the head advance — so it must be
// generous enough for a cold first sync (a fresh CI runner uploading the full
// catalog closure to a remote that has none of it yet), while still failing
// within a couple of minutes when the backend is genuinely unreachable. 15s was
// too tight for a cold full upload and silently dropped the very first publish.
const autoPushCatalogTimeout = 120 * time.Second

// autopushMarkerName is the debounce record (last auto-published catalog id)
// under the object-model cache dir — derived state, safe to delete.
const autopushMarkerName = "autopush-catalog"

// autopushEnabled reports whether catalog auto-publish is turned on from either
// source: the repo-level `intent.yaml execution.state.autopushCatalog` (committed,
// so a team enables it for everyone) OR the user-level `~/.orun/config.yaml
// cloud.catalog.autopush` (personal). Either being true enables it.
func autopushEnabled(intent *model.Intent) bool {
	if intent != nil && intent.Execution.State.AutopushCatalog {
		return true
	}
	if cfg, err := cliauth.LoadConfig(); err == nil && cfg != nil && cfg.Cloud.Catalog.AutoPush {
		return true
	}
	return false
}

// catalogAutoPublishScope reports whether a source scope is eligible for
// best-effort auto-publish: only the clean default branch. Feature branches,
// dirty trees, no-git, PRs, and tags are excluded so auto-sync never moves the
// project-wide head from non-canonical state.
func catalogAutoPublishScope(scope string) bool {
	return scope == catalogmodel.SourceScopeBranchMain
}

// maybeAutoPushCatalog publishes the just-resolved catalog after a successful
// plan when `execution.state.autopushCatalog` is set. It is best-effort and
// returns nothing: every precondition miss and every failure is a silent (or
// ORUN_VERBOSE) skip, so the plan's outcome is unaffected.
func maybeAutoPushCatalog(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	intent := loadIntentForCloudConfig()
	if !autopushEnabled(intent) {
		return
	}
	backendURL := resolveBackendURLWithConfig(intent, "")
	if backendURL == "" {
		return // autopush is meaningless without a backend to sync to
	}

	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil || !catalogAutoPublishScope(ws.Scope()) {
		return
	}

	// Debounce: read the catalog the plan just resolved and skip when it is
	// unchanged since the last successful auto-publish.
	_, refs, omRoot, err := openObjectModel()
	if err != nil {
		return
	}
	cur, err := refs.Read(ctx, catalogCurrentRef)
	if err != nil {
		return
	}
	if cur.Target == readAutopushMarker(omRoot) {
		return
	}

	pushCtx, cancel := context.WithTimeout(ctx, autoPushCatalogTimeout)
	defer cancel()
	if err := pushResolvedCatalog(pushCtx, backendURL, "", "", ""); err != nil {
		autopushVerbosef("catalog auto-sync skipped: %v", err)
		return
	}
	writeAutopushMarker(omRoot, cur.Target)
}

// autopushVerbosef surfaces a best-effort skip only when ORUN_VERBOSE is set —
// otherwise auto-sync is invisible on failure, mirroring the refresh hook.
func autopushVerbosef(format string, args ...interface{}) {
	if os.Getenv(verboseEnvVar) == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "⚠ "+format+"\n", args...)
}

func autopushMarkerPath(omRoot string) string {
	return filepath.Join(omRoot, "cache", autopushMarkerName)
}

// readAutopushMarker returns the last auto-published catalog id, or "" when no
// marker exists (which simply forfeits one debounce and triggers a push).
func readAutopushMarker(omRoot string) string {
	b, err := os.ReadFile(autopushMarkerPath(omRoot))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// writeAutopushMarker records the just-published catalog id. Best-effort: a
// write failure only forfeits the next debounce.
func writeAutopushMarker(omRoot, digest string) {
	path := autopushMarkerPath(omRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(digest), 0o644)
}
