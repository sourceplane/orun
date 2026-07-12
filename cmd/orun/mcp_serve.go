package main

// `orun mcp serve` startup (orun-mcp UM5): NEVER fail the handshake. The
// field report's second blocker was serve exiting 1 with zero stdout when
// auth/backends failed to resolve — indistinguishable from a crash to an
// MCP client. Startup is now best-effort: backend URL, token source, and
// workspace resolution each record their outcome into a mountReport, and
// `server.Serve` always begins. A fully degraded serve mounts only the
// built-in `connection_info` tool, whose output (and every per-call error)
// carries the exact fix. Absent and expired credentials behave the same:
// the server starts, the failure is a per-call verdict, never an exit.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/platformmcp"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workmcp"
)

// mcpMountReport is the serve-time resolution outcome, per concern. It
// feeds three renderings: the connection_info snapshot, the default one-line
// stderr note, and the --verbose multi-line summary.
type mcpMountReport struct {
	backendURL string // resolved backend URL; "" when none
	backendErr string // why, when none

	authState  string // ok | absent | expired | error
	authSource string // "GitHub Actions OIDC" | "ORUN_TOKEN" | "session"; "" when none resolved
	authDetail string // human detail: session user + expiry note, or the resolution error
	expiresAt  string // RFC3339 access-token expiry when known (session auth)

	workspace       string // resolved workspace; "" when none
	workspaceSource string // the winning resolution rung

	workMounted     bool
	workReason      string
	platformMounted bool
	platformReason  string
}

// planesSkipped marks both tool planes unmounted for one shared reason.
func (r mcpMountReport) planesSkipped(reason string) mcpMountReport {
	r.workMounted, r.platformMounted = false, false
	r.workReason, r.platformReason = reason, reason
	return r
}

// buildMcpMountReport is the best-effort serve preamble: mcpCloudClient's
// old resolution chain (backend URL → repo link → scope → token source),
// but NO outcome is fatal — every failure is recorded and the caller serves
// whatever mounted. The returned client is nil exactly when neither plane
// mounted.
func buildMcpMountReport(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, mcpMountReport) {
	rep := mcpMountReport{}
	intent := loadIntentForCloudConfig()
	backendURL, berr := requireBackendURL(intent, backendURLFlag)
	if berr != nil {
		rep.backendErr = berr.Error()
		backendURL = ""
	}
	rep.backendURL = backendURL

	// Workspace chain (best-effort — UM5: outside a git repo, or with an
	// unreadable link cache, serve degrades instead of exiting; the repo
	// link is only consulted when a backend URL resolved, as links are
	// stored per backend).
	linkOrg, linkProject := "", ""
	if backendURL != "" {
		if repo, err := resolveRepoContext(backendURL); err == nil && repo != nil {
			linkOrg, linkProject = repo.OrgID, repo.ProjectID
		}
	}
	intentOrg, intentProject, _ := intentScope(intent)
	scope := resolveScope(orgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
	envWS := preferWorkspace(os.Getenv(workspaceEnvVar), os.Getenv(orgEnvVar))
	rep.workspace = scope.OrgID
	_, rep.workspaceSource = resolveDoctorWorkspace(orgFlag, envWS, intentOrg, linkOrg)

	// Auth: which source resolves (OIDC in CI > ORUN_TOKEN > local session)
	// — attempted even without a backend URL so the report stays truthful
	// (OIDC/ORUN_TOKEN resolve backend-free; session auth needs the URL).
	resolved, err := remotestate.ResolveAuth(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		Org:          scope.OrgID,
	})
	if err != nil {
		switch {
		case isNoLoginErr(err):
			rep.authState, rep.authDetail = "absent", errNotLoggedIn().Error()
		case backendURL == "":
			// Session auth cannot even be attempted without a backend URL;
			// read the session file directly so the report tells the truth
			// about what IS on disk (states only — never token material).
			if creds, lerr := cliauth.LoadSession(); lerr == nil && creds != nil {
				rep.authSource = "session"
				rep.authState, rep.authDetail, rep.expiresAt = sessionMountState(creds, time.Now())
			} else {
				rep.authState, rep.authDetail = "absent", errNotLoggedIn().Error()
			}
		default:
			rep.authState, rep.authDetail = "error", err.Error()
		}
	} else {
		rep.authState = "ok"
		switch resolved.ResolvedMode {
		case "oidc":
			rep.authSource = "GitHub Actions OIDC"
		case "static":
			rep.authSource = "ORUN_TOKEN"
		default:
			rep.authSource = "session"
			// A fully expired session still resolves a token source (expiry
			// bites at first use); surface it as state "expired" here so
			// connection_info matches what per-call errors will say.
			creds, _ := cliauth.LoadSession()
			rep.authState, rep.authDetail, rep.expiresAt = sessionMountState(creds, time.Now())
		}
	}

	switch {
	case rep.backendURL == "":
		return nil, rep.planesSkipped("no backend URL resolved — set ORUN_BACKEND_URL or pass --backend-url")
	case rep.authState == "absent" || rep.authState == "error":
		return nil, rep.planesSkipped("auth did not resolve — " + rep.authDetail)
	}

	// Auth resolved (ok, or expired — expired mounts too: the planes fail
	// per-call with the login hint, the same degradation shape as absent).
	rep.platformMounted, rep.platformReason = true, "auth resolved ("+rep.authSource+")"
	if rep.workspace != "" {
		rep.workMounted, rep.workReason = true, fmt.Sprintf("workspace %s (from %s)", rep.workspace, rep.workspaceSource)
	} else {
		rep.workReason = "no workspace resolved — pass --workspace or run `orun cloud link`"
	}
	return remotestate.NewClientWithScope(backendURL, version, resolved.TokenSource, scope), rep
}

// sessionMountState classifies the on-disk session: structured state for
// the mount report + connection_info, mirroring the doctor's verdicts. An
// expired access token with a live refresh token is ok (it refreshes on
// first use); only a fully expired session is "expired". Never returns
// token material.
func sessionMountState(creds *cliauth.Credentials, now time.Time) (state, detail, expiresAt string) {
	if creds == nil {
		return "absent", "no local session; run `orun auth login`", ""
	}
	user := creds.DisplayUser()
	if user == "" {
		user = "unknown user"
	}
	access, refresh := creds.AccessExpiryTime(), creds.RefreshExpiryTime()
	if creds.AccessToken != "" && (access.IsZero() || access.After(now)) {
		if !access.IsZero() {
			expiresAt = access.Format(time.RFC3339)
		}
		return "ok", "local session for " + user, expiresAt
	}
	if strings.TrimSpace(creds.RefreshToken) != "" && (refresh.IsZero() || refresh.After(now)) {
		return "ok", "local session for " + user + "; access token expired, refresh token live — it refreshes on first use", ""
	}
	return "expired", "local session for " + user + " has fully expired; run `orun auth login`", ""
}

// connectionInfo maps the mount report onto the built-in tool's snapshot.
func (r mcpMountReport) connectionInfo() mcpserve.ConnectionInfo {
	info := mcpserve.ConnectionInfo{
		AuthState:  r.authState,
		AuthSource: r.authSource,
		ExpiresAt:  r.expiresAt,
		BackendURL: r.backendURL,
		Work:       mcpserve.PlaneMount{Mounted: r.workMounted, Reason: r.workReason},
		Platform:   mcpserve.PlaneMount{Mounted: r.platformMounted, Reason: r.platformReason},
	}
	switch {
	case r.backendURL == "":
		info.Fix = "set ORUN_BACKEND_URL or run `orun auth login`"
	case r.authState != "ok":
		info.Fix = "run `orun auth login`"
	}
	return info
}

// assembleMcpProviders builds the provider list the composed server mounts:
// work when a workspace resolved, platform when auth resolved, and the
// built-in connection_info provider ALWAYS — the degraded server's only
// tool, and the fully mounted server's extra one.
func assembleMcpProviders(client *remotestate.Client, rep mcpMountReport, readOnly bool) []mcpserve.ToolProvider {
	var providers []mcpserve.ToolProvider
	if rep.workMounted {
		providers = append(providers, &workmcp.Server{API: client, Workspace: rep.workspace})
	}
	if rep.platformMounted {
		providers = append(providers, &platformmcp.Provider{API: client, DefaultWorkspace: rep.workspace, ReadOnly: readOnly})
	}
	providers = append(providers, &mcpserve.ConnectionInfoProvider{Info: rep.connectionInfo()})
	return providers
}

// mcpServeLine is the default single stderr line: what mounted, and — when
// degraded — the fix, right where the operator is looking.
func mcpServeLine(r mcpMountReport) string {
	switch {
	case r.workMounted && r.platformMounted:
		return "orun MCP serving on stdio (workspace " + r.workspace + "; work + platform tools)"
	case r.platformMounted:
		return "orun MCP serving on stdio (no workspace resolved: platform tools only — pass --workspace or link the repo to mount work tools)"
	case r.backendURL == "":
		return "orun MCP serving on stdio (degraded: no backend URL — set ORUN_BACKEND_URL or run `orun auth login`; only connection_info is available)"
	default:
		return "orun MCP serving on stdio (degraded: not logged in — run `orun auth login`; only connection_info is available)"
	}
}

// mcpVerboseSummary renders the --verbose multi-line startup summary:
// backend URL, auth source + state (+ expiry when known), workspace +
// source, and each plane's mount outcome with its reason.
func mcpVerboseSummary(r mcpMountReport) string {
	var b strings.Builder
	b.WriteString("orun MCP startup summary:\n")
	line := func(k, v string) { fmt.Fprintf(&b, "  %-15s %s\n", k+":", v) }
	if r.backendURL != "" {
		line("backend URL", r.backendURL)
	} else {
		line("backend URL", "none — "+r.backendErr)
	}
	auth := r.authState
	if r.authSource != "" {
		auth += " (" + r.authSource + ")"
	}
	if r.authDetail != "" {
		auth += " — " + r.authDetail
	}
	line("auth", auth)
	if r.expiresAt != "" {
		line("token expiry", r.expiresAt)
	}
	if r.workspace != "" {
		line("workspace", r.workspace+" (from "+r.workspaceSource+")")
	} else {
		line("workspace", "none resolved (checked --workspace, ORUN_WORKSPACE/ORUN_ORG, intent.yaml execution.state, the repo link)")
	}
	plane := func(mounted bool, reason string) string {
		if mounted {
			return "mounted — " + reason
		}
		return "skipped — " + reason
	}
	line("plane work", plane(r.workMounted, r.workReason))
	line("plane platform", plane(r.platformMounted, r.platformReason))
	line("plane server", "mounted — connection_info (always present)")
	return b.String()
}

func registerMcpServeCommand(parent *cobra.Command, counts mcpRosterCounts) {
	var (
		workspace  string
		backendURL string
		readOnly   bool
		verbose    bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the orun MCP over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// UM5: never exit before the handshake. Resolution is
			// best-effort; every failure degrades the mount (recorded in
			// the report, reported by connection_info) instead of failing
			// the command. Stdout stays protocol-pure; diagnostics go to
			// stderr.
			client, rep := buildMcpMountReport(cmd.Context(), backendURL, workspace)
			server := &mcpserve.Server{Providers: assembleMcpProviders(client, rep, readOnly), Version: version}
			if verbose {
				fmt.Fprint(cmd.ErrOrStderr(), mcpVerboseSummary(rep))
			}
			fmt.Fprintln(cmd.ErrOrStderr(), mcpServeLine(rep))
			return server.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, fmt.Sprintf("serve only the platform plane's read tools (drops the %d platform writes; work tools are mutator-shaped by design — WP-6 — and unaffected)", counts.platformWrites))
	cmd.Flags().BoolVar(&verbose, "verbose", false, "print a multi-line startup summary to stderr (planes mounted and why, auth source, token expiry, workspace source, backend URL)")
	parent.AddCommand(cmd)
}
