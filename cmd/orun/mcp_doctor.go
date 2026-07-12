package main

// `orun mcp doctor` (orun-mcp UM4): the one-command diagnosis for "my MCP
// client can't see/reach the orun server", mirroring `orun cloud check`'s
// ✓/✗-with-one-actionable-line output style. It validates, in order: the
// binary (and the field report's P0 — a stale `orun` on PATH silently
// lacking `mcp serve`), auth resolution + session expiry (never printing
// secret material), the workspace resolution chain, and backend sanity
// (one platform route + one work route probed with the resolved
// credential), then prints the exact absolute-path registration line.
// Strictly read-only: GET probes only, no writes, no token minting beyond
// the normal resolution any command performs.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
)

// doctorCheck is one row of the diagnosis: a check name, the verdict, and
// exactly one actionable line.
type doctorCheck struct {
	name string
	ok   bool
	warn bool // ok-with-caveat: rendered as "!" and does not fail the run
	line string
}

func registerMcpDoctorCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the MCP serve preconditions (binary, auth, workspace, backend)",
		Long: `Diagnose why an MCP client can't see or reach 'orun mcp serve'.

Checks, in order: this binary (and whether a DIFFERENT 'orun' on PATH would
shadow it — the most common field failure), auth resolution and session
expiry (no secrets printed), the workspace resolution chain
(--workspace > ORUN_WORKSPACE/ORUN_ORG > intent.yaml > repo link), and
backend sanity (one platform route and one work route probed with the
resolved credential — a 404 on a platform route means the backend URL is
not an Orun Cloud API endpoint). Ends with the exact absolute-path
registration line to paste into your MCP client. Exits non-zero when any
check fails. Read-only: GET probes only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpDoctor(cmd, backendURL, workspace)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the resolution chain)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	parent.AddCommand(cmd)
}

func runMcpDoctor(cmd *cobra.Command, backendURLFlag, workspaceFlag string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	// (a) binary capability + stale-PATH detection.
	execPath, checks := doctorBinaryChecks()

	backendURL, berr := requireBackendURL(loadIntentForCloudConfig(), backendURLFlag)
	var ws string
	var wsCheck doctorCheck
	if berr != nil {
		// Everything downstream needs a backend URL; report the miss once and
		// mark the dependent checks skipped rather than repeating the error.
		checks = append(checks,
			doctorCheck{name: "auth", line: berr.Error()},
			doctorCheck{name: "workspace", line: "skipped — no backend URL resolved"},
			doctorCheck{name: "platform API", line: "skipped — no backend URL resolved"},
			doctorCheck{name: "work API", line: "skipped — no backend URL resolved"},
		)
	} else {
		// (c) resolved first because the OIDC exchange claims the org, but
		// reported after auth to keep the plan's check order.
		ws, wsCheck = doctorWorkspace(backendURL, workspaceFlag)

		// (b) auth: which token source resolves, and (for a session) expiry.
		tokenSrc, authCheck := doctorAuth(ctx, backendURL, ws)
		checks = append(checks, authCheck, wsCheck)

		// (d) backend sanity: one platform route, one work route.
		if tokenSrc == nil {
			checks = append(checks,
				doctorCheck{name: "platform API", line: "skipped — auth did not resolve"},
				doctorCheck{name: "work API", line: "skipped — auth did not resolve"},
			)
		} else {
			client := remotestate.NewClientWithScope(backendURL, version, tokenSrc, remotestate.Scope{OrgID: ws})
			checks = append(checks, doctorBackendProbes(ctx, client, backendURL, ws)...)
		}
	}

	out := cmd.OutOrStdout()
	color := ui.ColorEnabledForWriter(os.Stdout)
	failed := 0
	for _, c := range checks {
		mark := ui.Green(color, "✓")
		switch {
		case !c.ok:
			mark = ui.Yellow(color, "✗")
			failed++
		case c.warn:
			mark = ui.Yellow(color, "!")
		}
		fmt.Fprintf(out, "%s %-12s %s\n", mark, c.name, c.line)
	}

	// (e) the copy-pastable registration line — always with the ABSOLUTE
	// path, so a stale `orun` earlier on the client's PATH cannot shadow
	// this binary (the field report's P0).
	fmt.Fprintf(out, "\nregister this binary with your MCP client (absolute path — never plain `orun`):\n  claude mcp add orun -- %s mcp serve\n", execPath)

	if failed > 0 {
		return fmt.Errorf("mcp doctor: %d of %d checks failed", failed, len(checks))
	}
	return nil
}

// doctorBinaryChecks reports the running binary — which trivially HAS `mcp
// serve` (this command ships in it), so the useful facts are its absolute
// path and version — and warns when `orun` on PATH resolves to a different
// file: an MCP client registered with plain `orun` launches THAT binary,
// which may predate the mcp command entirely.
func doctorBinaryChecks() (string, []doctorCheck) {
	execPath, err := os.Executable()
	if err != nil {
		return "orun", []doctorCheck{{name: "binary", ok: true, warn: true,
			line: fmt.Sprintf("this binary serves MCP (version %s) but its path could not be determined: %v", version, err)}}
	}
	if resolved, rerr := filepath.EvalSymlinks(execPath); rerr == nil {
		execPath = resolved
	}
	checks := []doctorCheck{{name: "binary", ok: true,
		line: fmt.Sprintf("%s (version %s) has `mcp serve`", execPath, version)}}
	if warn := doctorPathMismatch(execPath, exec.LookPath); warn != nil {
		checks = append(checks, *warn)
	}
	return execPath, checks
}

// doctorPathMismatch returns a warning row when plain `orun` on PATH is not
// this binary (nil when it is). lookPath is injected for tests.
func doctorPathMismatch(execPath string, lookPath func(string) (string, error)) *doctorCheck {
	onPath, err := lookPath("orun")
	if err != nil {
		return &doctorCheck{name: "PATH", ok: true, warn: true,
			line: "no `orun` on PATH — clients must be registered with the absolute path below"}
	}
	if resolved, rerr := filepath.EvalSymlinks(onPath); rerr == nil {
		onPath = resolved
	}
	if sameDoctorFile(onPath, execPath) {
		return nil
	}
	return &doctorCheck{name: "PATH", ok: true, warn: true,
		line: fmt.Sprintf("`orun` on PATH is %s — NOT this binary; a client registered with plain `orun` runs that one (it may lack `mcp serve`); use the absolute registration line below", onPath)}
}

// sameDoctorFile reports whether two paths name the same file (path
// equality first, then an inode comparison to see through hardlinks).
func sameDoctorFile(a, b string) bool {
	if a == b {
		return true
	}
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// doctorAuth resolves the token source exactly the way `mcp serve` does
// (OIDC in CI > ORUN_TOKEN > local session) and describes it. For a
// session it additionally reports expiry. No secret material is printed
// and no token is minted beyond that normal resolution.
func doctorAuth(ctx context.Context, backendURL, org string) (remotestate.TokenSource, doctorCheck) {
	resolved, err := remotestate.ResolveAuth(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		Org:          org,
	})
	if err != nil {
		if isNoLoginErr(err) {
			err = errNotLoggedIn()
		}
		return nil, doctorCheck{name: "auth", line: err.Error()}
	}
	switch resolved.ResolvedMode {
	case "oidc":
		return resolved.TokenSource, doctorCheck{name: "auth", ok: true, line: "GitHub Actions OIDC exchange (CI)"}
	case "static":
		return resolved.TokenSource, doctorCheck{name: "auth", ok: true, line: "ORUN_TOKEN (static bearer)"}
	}
	creds, _ := cliauth.LoadSession()
	ok, line := describeSessionAuth(creds, time.Now())
	return resolved.TokenSource, doctorCheck{name: "auth", ok: ok, line: line}
}

// describeSessionAuth renders the session-auth verdict: user + expiry,
// never token material. An expired access token with a live refresh token
// is healthy (it refreshes on first use); a fully expired session fails.
func describeSessionAuth(creds *cliauth.Credentials, now time.Time) (bool, string) {
	if creds == nil {
		return false, "no local session; run `orun auth login`"
	}
	user := creds.DisplayUser()
	if user == "" {
		user = "unknown user"
	}
	access, refresh := creds.AccessExpiryTime(), creds.RefreshExpiryTime()
	if creds.AccessToken != "" && (access.IsZero() || access.After(now)) {
		line := fmt.Sprintf("local session for %s", user)
		if !access.IsZero() {
			line += fmt.Sprintf("; access token valid until %s", access.Format(time.RFC3339))
		}
		return true, line
	}
	if strings.TrimSpace(creds.RefreshToken) != "" && (refresh.IsZero() || refresh.After(now)) {
		return true, fmt.Sprintf("local session for %s; access token expired, refresh token live — it refreshes on first use", user)
	}
	return false, fmt.Sprintf("local session for %s has fully expired; run `orun auth login`", user)
}

// doctorWorkspace resolves the workspace exactly like `mcp serve`
// (--workspace > ORUN_WORKSPACE/ORUN_ORG > intent.yaml execution.state >
// the cached repo link) and names the rung that supplied it. No workspace
// is a warning, not a failure: serve still mounts the platform plane.
func doctorWorkspace(backendURL, flagWS string) (string, doctorCheck) {
	intentOrg, _, _ := intentScope(loadIntentForCloudConfig())
	linkOrg := ""
	if repo, err := resolveRepoContext(backendURL); err == nil && repo != nil {
		linkOrg = repo.OrgID
	}
	envWS := preferWorkspace(os.Getenv(workspaceEnvVar), os.Getenv(orgEnvVar))
	ws, source := resolveDoctorWorkspace(flagWS, envWS, intentOrg, linkOrg)
	if ws == "" {
		return "", doctorCheck{name: "workspace", ok: true, warn: true,
			line: "none resolved (checked --workspace, ORUN_WORKSPACE/ORUN_ORG, intent.yaml execution.state, the repo link) — serve mounts platform tools only; pass --workspace or run `orun cloud link` to mount work tools"}
	}
	return ws, doctorCheck{name: "workspace", ok: true, line: fmt.Sprintf("%s (from %s)", ws, source)}
}

// resolveDoctorWorkspace is resolveScope's org chain with the winning rung
// named — same precedence, one shared truth to report.
func resolveDoctorWorkspace(flagWS, envWS, intentWS, linkWS string) (value, source string) {
	for _, cand := range []struct{ v, src string }{
		{flagWS, "--workspace"},
		{envWS, "ORUN_WORKSPACE/ORUN_ORG"},
		{intentWS, "intent.yaml execution.state"},
		{linkWS, "the cached repo link"},
	} {
		if v := strings.TrimSpace(cand.v); v != "" {
			return v, cand.src
		}
	}
	return "", ""
}

// doctorBackendProbes GETs one platform route (/v1/auth/profile — every
// Orun Cloud api-edge serves it) and, when a workspace resolved, one work
// route, with the resolved credential, and classifies each plane's answer.
func doctorBackendProbes(ctx context.Context, client *remotestate.Client, backendURL, ws string) []doctorCheck {
	_, perr := client.GetAuthProfile(ctx)
	checks := []doctorCheck{classifyDoctorProbe("platform API", "GET "+backendURL+"/v1/auth/profile", perr, true)}
	if ws == "" {
		checks = append(checks, doctorCheck{name: "work API", ok: true, warn: true,
			line: "skipped — no workspace resolved (work tools would not mount)"})
		return checks
	}
	_, werr := client.GetWorkSummary(ctx)
	checks = append(checks, classifyDoctorProbe("work API", "GET work summary (workspace "+ws+")", werr, false))
	return checks
}

// classifyDoctorProbe turns one probe result into a row. platformRoute
// flags not_found specially: /v1/auth/profile exists on every Orun Cloud
// api-edge, so a 404 there means the configured URL is not an Orun Cloud
// API endpoint at all (the legacy state backend's unhelpful NOT_FOUND —
// the field evaluation's most misleading failure).
func classifyDoctorProbe(name, route string, err error, platformRoute bool) doctorCheck {
	if err == nil {
		return doctorCheck{name: name, ok: true, line: route + " → ok"}
	}
	var apiErr *remotestate.APIError
	if errors.As(err, &apiErr) {
		switch {
		case platformRoute && isDoctorNotFound(apiErr):
			return doctorCheck{name: name,
				line: fmt.Sprintf("%s → 404: the backend URL is not an Orun Cloud API endpoint (this route exists on api-edge); fix the backend URL, then check `orun cloud status` and `orun auth login`", route)}
		case apiErr.IsAuth():
			return doctorCheck{name: name,
				line: fmt.Sprintf("%s → %s: the credential was rejected; run `orun auth login`", route, apiErr.Code)}
		default:
			return doctorCheck{name: name, line: fmt.Sprintf("%s → %v", route, err)}
		}
	}
	return doctorCheck{name: name, line: fmt.Sprintf("%s → %v (backend unreachable?)", route, err)}
}

// isDoctorNotFound recognises both the platform's lowercase not_found and
// the legacy backend's status-derived NOT_FOUND.
func isDoctorNotFound(e *remotestate.APIError) bool {
	return strings.EqualFold(e.Code, "not_found") || e.Status == 404
}
