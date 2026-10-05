package main

// work_autosync.go is the plan-time half of the work sync (saas-work-gitops
// WG-24): the same mechanism as the catalog auto-publish beside it. When
// intent.yaml sets `work.sync: on-merge`, a successful `orun plan` on the
// clean default branch reconciles the declared work tree into Orunbase —
// no separate job, in any repository that runs orun. The guardrails are
// the catalog's: default branch and clean tree only; debounced by commit,
// so repeated plans on the same head cost nothing; never at the cost of
// the plan's exit code. The one difference: a refused write is a
// declaration problem the author must see, so it is printed as a warning
// whether or not ORUN_VERBOSE is set, and the next plan on main resumes
// from the tree.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// autoSyncWorkTimeout bounds one plan-time sync: a declaration of several
// epics makes ~80 writes at ~1.5s each through the edge.
const autoSyncWorkTimeout = 10 * time.Minute

// autosyncWorkMarkerName records the last commit the plan-time sync
// completed for — derived state under the object-model cache, safe to delete.
const autosyncWorkMarkerName = "autosync-work"

// workAutoSyncEnabled reports whether the plan-time sync is on: the
// committed intent says `work.sync: on-merge`.
func workAutoSyncEnabled(intent *model.Intent) bool {
	if intent == nil {
		return false
	}
	layout := intent.WorkLayout()
	return layout.Declared() && layout.Sync == model.WorkSyncOnMerge
}

// maybeAutoSyncWork reconciles the declared work tree after a successful
// plan when intent says on-merge. Best-effort: every precondition miss is a
// silent (or ORUN_VERBOSE) skip and the plan's outcome is unaffected; a
// refused write is printed, because the author has a file to fix.
func maybeAutoSyncWork(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	intent := loadIntentForCloudConfig()
	if !workAutoSyncEnabled(intent) {
		autopushVerbosef("work auto-sync: disabled")
		return
	}
	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		autopushVerbosef("work auto-sync: workspace root: %v", err)
		return
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil || !catalogAutoPublishScope(ws) {
		scope := "?"
		if err == nil {
			scope = ws.Scope()
		}
		autopushVerbosef("work auto-sync: scope gate (err=%v scope=%s)", err, scope)
		return
	}
	_, _, omRoot, err := openObjectModel()
	if err != nil {
		autopushVerbosef("work auto-sync: object model: %v", err)
		return
	}
	if ws.HeadRevision != "" && ws.HeadRevision == readAutosyncWorkMarker(omRoot) {
		autopushVerbosef("work auto-sync: debounced (same commit)")
		return
	}
	client, err := cloudClient(ctx, "", "")
	if err != nil {
		autopushVerbosef("work auto-sync skipped: %v", err)
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, autoSyncWorkTimeout)
	defer cancel()
	root := taskDocRoot()
	tree, err := loadWorkTree(root, intent.WorkLayout())
	if err != nil {
		// A broken declaration reached main past the work-manifest check —
		// say so; the plan itself is unaffected.
		fmt.Fprintf(os.Stderr, "⚠ work sync: %v — `orun work check` names the problems\n", err)
		return
	}
	fmt.Fprintln(os.Stdout, "work sync (intent work.sync: on-merge)")
	if _, err := runWorkSyncTree(syncCtx, root, tree, client, ws.Repo, false, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ work sync: %v\n", err)
		return
	}
	writeAutosyncWorkMarker(omRoot, ws.HeadRevision)
}

func autosyncWorkMarkerPath(omRoot string) string {
	return filepath.Join(omRoot, "cache", autosyncWorkMarkerName)
}

func readAutosyncWorkMarker(omRoot string) string {
	b, err := os.ReadFile(autosyncWorkMarkerPath(omRoot))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeAutosyncWorkMarker(omRoot, sha string) {
	path := autosyncWorkMarkerPath(omRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(sha), 0o644)
}
