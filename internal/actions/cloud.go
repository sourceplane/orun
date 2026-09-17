package actions

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
)

// Reaching the platform from an action (orun-bootstrap-engine BE-O5).
//
// The CLI resolves its workspace from four places — a flag, the environment,
// intent.yaml, and the repo link — because a person at a terminal may have set
// any of them. An action is not a person at a terminal: it runs inside a
// bootstrap that already knows which workspace it is building into, and the
// blueprint says so. So the resolution here is deliberately the narrow one:
// the `org` parameter, then the environment. Anything cleverer would let an
// action act on a workspace its blueprint never named.
//
// The environment is read as workspaceFromEnv spells it: ORUN_WORKSPACE first,
// ORUN_ORG second. Reading only the alias made the estate's own leading
// spelling a no-op for hooks — an operator who exported ORUN_WORKSPACE, which
// every other surface prefers, was told to set ORUN_ORG.
//
// The backend URL is ORUN_BACKEND_URL or the `backendUrl` parameter — the same
// single source of truth a product's intent.yaml declares.

// actionsVersion is the client version string these actions present. It is not
// the binary's release version (which lives in the command layer); it names the
// caller, which is what a server-side log needs to attribute a request.
const actionsVersion = "orun-actions/1"

// workspaceFromEnv reads the ambient workspace the way every CLI surface does:
// ORUN_WORKSPACE is the leading spelling, ORUN_ORG the retained alias
// (saas-workspaces A4). A sandbox sets ORUN_WORKSPACE for every session.
func workspaceFromEnv() string {
	if w := strings.TrimSpace(os.Getenv("ORUN_WORKSPACE")); w != "" {
		return w
	}
	return strings.TrimSpace(os.Getenv("ORUN_ORG"))
}

// errNoWorkspace names both spellings, and the parameter that beats them, so
// the fix is in the message rather than in the source.
func errNoWorkspace() error {
	return fmt.Errorf("no workspace: pass `org`, or set ORUN_WORKSPACE (or ORUN_ORG)")
}

// backendURL resolves the platform address for an action: the `backendUrl`
// parameter, then ORUN_BACKEND_URL, then the same default the CLI dials. It
// cannot fail — a hook started by a CLI that reached the platform must be able
// to reach it too, and before this it could not: the command layer grew a
// default and the actions kept refusing, so a bootstrap died at its first
// platform-facing hook on a machine where the CLI itself worked.
func backendURL(in Input) string {
	if b := strings.TrimSpace(StringParam(in, "backendUrl")); b != "" {
		return b
	}
	if b := strings.TrimSpace(os.Getenv("ORUN_BACKEND_URL")); b != "" {
		return b
	}
	return remotestate.DefaultCloudURL
}

// cloudClient builds a platform client for an action.
func cloudClient(ctx context.Context, in Input) (*remotestate.Client, string, error) {
	org := strings.TrimSpace(StringParam(in, "org"))
	if org == "" {
		org = workspaceFromEnv()
	}
	if org == "" {
		return nil, "", errNoWorkspace()
	}
	backend := backendURL(in)
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL:   backend,
		Version:      actionsVersion,
		Interactive:  false, // an action never prompts; it is running unattended
		RequireLogin: true,
		Org:          org,
	})
	if err != nil {
		return nil, "", fmt.Errorf("resolving a credential for %s: %w", org, err)
	}
	client := remotestate.NewClientWithScope(backend, actionsVersion, tokenSrc, remotestate.Scope{OrgID: org})
	return client, org, nil
}

// orgParams are the two parameters every platform-facing action declares.
func orgParams() []Param {
	return []Param{
		{Name: "org", Type: ParamString,
			Description: "workspace id (ws_…/org_…) or slug; defaults to ORUN_WORKSPACE, then ORUN_ORG"},
		{Name: "backendUrl", Type: ParamString,
			Description: "platform base URL; defaults to ORUN_BACKEND_URL, then Orun Cloud"},
	}
}
