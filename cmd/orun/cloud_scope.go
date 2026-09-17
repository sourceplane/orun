package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The workspace-scoped cloud client and the three flags every command that
// speaks to a workspace carries. Carved out of the work-plane CLI group at
// the teardown (orun-work-teardown WT2): `orun skills` and `orun pr` were
// built on these helpers and outlive the group that happened to define
// them.

// cloudClient resolves scope + auth and builds the workspace-scoped cloud
// client — the same preamble as catalog push (flag > env > intent > cached
// link).
func cloudClient(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, error) {
	backendURL, err := requireBackendURL(nil, backendURLFlag)
	if err != nil {
		return nil, err
	}
	// THE REPO LINK IS A FALLBACK, NOT A PREREQUISITE. resolveRepoContext
	// reads the cached link for the checkout you are standing in, which is one
	// of four ways a workspace can be named — and the weakest. Treating its
	// failure as fatal meant `orun baseline new --local --workspace ws_…`,
	// which names the workspace outright and builds into --out, died outside a
	// git repo with `detect git remote.origin.url: exit status 1` (hit live).
	// The same command one directory over, inside any checkout, worked.
	// `mcp serve` already degrades here rather than exiting (UM5); so does
	// this now, and the no-workspace error below stays the one that fires when
	// nothing names one.
	linkOrg, linkProject := "", ""
	repo, repoErr := resolveRepoContext(backendURL)
	if repoErr == nil && repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	intentOrg, intentProject, _ := intentScope(loadIntentForCloudConfig())
	scope := resolveScope(orgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
	if scope.OrgID == "" {
		// Nothing named a workspace AND the link could not be consulted: say
		// why, so "link the repo" does not read as advice that was already
		// tried and silently failed.
		if repoErr != nil {
			return nil, fmt.Errorf("no workspace resolved; pass --workspace or set %s (the repo link was not consulted: %v)", workspaceEnvVar, repoErr)
		}
		return nil, fmt.Errorf("no workspace resolved; pass --workspace or link the repo (orun auth login)")
	}
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		Org:          scope.OrgID,
	})
	if err != nil {
		if isNoLoginErr(err) {
			return nil, errNotLoggedIn()
		}
		return nil, fmt.Errorf("remote state auth: %w", err)
	}
	// THE SOURCE ABOVE IS A HANDLE, NOT A TOKEN. ResolveTokenSource proves a
	// session is stored; it does not prove the session can still mint
	// anything. A stored session that has expired past its refresh, or been
	// revoked, sailed through here and failed inside the FIRST REQUEST as
	// "resolving auth token: file does not exist" (live, on `baseline new`) —
	// the raw store error, with no mention of what to do. Read one token now,
	// so that case fails at the door with the same "run `orun auth login`"
	// every other not-logged-in case gets.
	if err := validateToken(ctx, tokenSrc); err != nil {
		return nil, err
	}
	return remotestate.NewClientWithScope(backendURL, version, tokenSrc, scope), nil
}

// addCloudScopeFlags registers the three flags a workspace-scoped
// subcommand carries: --workspace, --backend-url, --json.
func addCloudScopeFlags(cmd *cobra.Command, workspace, backendURL *string, asJSON *bool) {
	cmd.Flags().StringVar(workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(asJSON, "json", false, "emit JSON")
}

// encodeJSON is the one JSON idiom these commands share: pretty-encoded
// response structs.
func encodeJSON(cmd *cobra.Command, v interface{}) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
