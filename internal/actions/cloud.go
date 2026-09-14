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
// The CLI resolves its workspace from four places — a flag, ORUN_ORG,
// intent.yaml, and the repo link — because a person at a terminal may have set
// any of them. An action is not a person at a terminal: it runs inside a
// bootstrap that already knows which workspace it is building into, and the
// blueprint says so. So the resolution here is deliberately the narrow one:
// the `org` parameter, then ORUN_ORG. Anything cleverer would let an action
// act on a workspace its blueprint never named.
//
// The backend URL is ORUN_BACKEND_URL or the `backendUrl` parameter — the same
// single source of truth a product's intent.yaml declares.

// actionsVersion is the client version string these actions present. It is not
// the binary's release version (which lives in the command layer); it names the
// caller, which is what a server-side log needs to attribute a request.
const actionsVersion = "orun-actions/1"

// cloudClient builds a platform client for an action.
func cloudClient(ctx context.Context, in Input) (*remotestate.Client, string, error) {
	org := strings.TrimSpace(StringParam(in, "org"))
	if org == "" {
		org = strings.TrimSpace(os.Getenv("ORUN_ORG"))
	}
	if org == "" {
		return nil, "", fmt.Errorf("no workspace: pass `org` or set ORUN_ORG")
	}
	backend := strings.TrimSpace(StringParam(in, "backendUrl"))
	if backend == "" {
		backend = strings.TrimSpace(os.Getenv("ORUN_BACKEND_URL"))
	}
	if backend == "" {
		return nil, "", fmt.Errorf("no backend URL: pass `backendUrl` or set ORUN_BACKEND_URL")
	}
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
			Description: "workspace id (ws_…/org_…) or slug; defaults to ORUN_ORG"},
		{Name: "backendUrl", Type: ParamString,
			Description: "platform base URL; defaults to ORUN_BACKEND_URL"},
	}
}
