package main

// The ACCOUNT a workspace belongs to. A login that owns several Orun Cloud
// accounts must say which one a new workspace lands in (the platform refuses
// to guess: 412 precondition_failed, "You own more than one account. Name the
// account this workspace belongs to (accountId)."). `--account <ws_…|slug>`
// on `orun workspace create` names it; without the flag the refusal is turned
// into the list of accounts the login owns and the exact command to rerun.
// `orun workspace list` shows the owning account when the backend reports it.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/ui"
)

// workspaceCreateOpts is what `orun workspace create` (and its older spelling
// under `orun cloud`) collects before calling the platform.
type workspaceCreateOpts struct {
	Name string
	Slug string
	// Account is the --account value: an account id or slug, empty to let the
	// platform use the login's default (only) account.
	Account string
	// CommandPath is the spelling that was invoked ("orun workspace create" or
	// "orun cloud workspace create"), echoed in the rerun hint so it is
	// copy-paste runnable as typed.
	CommandPath string
}

// accountListTimeout bounds the best-effort lookups (`workspace list`'s
// account column, the rerun hint) so an unreachable backend costs seconds,
// not a hang, on a command that otherwise works from local state.
const accountListTimeout = 5 * time.Second

// resolveAccountArg turns the --account value into the id POST
// /v1/organizations takes, plus the account's slug when the listing knows it.
// A slug is matched against the accounts the session can see; an id is
// accepted as typed. When the backend cannot list accounts (older platforms)
// the value is sent through unchanged and the server has the final say.
func resolveAccountArg(ctx context.Context, client *cliauth.BackendClient, token, target string) (id, slug string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", nil
	}
	accounts, listErr := client.ListMyAccounts(ctx, token)
	if listErr != nil || len(accounts) == 0 {
		return target, "", nil
	}
	for _, a := range accounts {
		if a.Matches(target) {
			return a.ID, a.Slug, nil
		}
	}
	return "", "", errUnknownAccount(target, accounts)
}

func errUnknownAccount(target string, accounts []cliauth.AccountRef) error {
	var b strings.Builder
	fmt.Fprintf(&b, "no account %q on this session", target)
	labels := make([]string, 0, len(accounts))
	for _, a := range accounts {
		labels = append(labels, a.Label())
	}
	if suggestion := ui.SuggestMatch(target, labels); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean: --account %s", suggestion)
	}
	fmt.Fprintf(&b, "\n\naccounts: %s", strings.Join(labels, ", "))
	return fmt.Errorf("%s", b.String())
}

// ownedAccounts lists the accounts the login may create a workspace in, for
// the rerun hint: the ones the refusal itself names, else GET /v1/accounts
// filtered to the owner role (every account when the backend reports no
// roles), else the distinct accounts the session's workspaces belong to.
// Empty when none of those knows.
func ownedAccounts(ctx context.Context, client *cliauth.BackendClient, token string, cause error) []cliauth.AccountRef {
	if named := cliauth.AccountsFromErrorDetails(cause); len(named) > 0 {
		return named
	}
	ctx, cancel := context.WithTimeout(ctx, accountListTimeout)
	defer cancel()
	if listed, err := client.ListMyAccounts(ctx, token); err == nil && len(listed) > 0 {
		owned := make([]cliauth.AccountRef, 0, len(listed))
		anyRole := false
		for _, a := range listed {
			if strings.TrimSpace(a.Role) != "" {
				anyRole = true
			}
			if strings.EqualFold(strings.TrimSpace(a.Role), "owner") {
				owned = append(owned, a)
			}
		}
		if !anyRole {
			return listed
		}
		if len(owned) > 0 {
			return owned
		}
	}
	seen := map[string]bool{}
	var out []cliauth.AccountRef
	for _, o := range sessionOrgs() {
		id := strings.TrimSpace(o.AccountID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, cliauth.AccountRef{ID: id, Slug: o.AccountSlug})
	}
	return out
}

// errWorkspaceNeedsAccount wraps the platform's "name the account" refusal
// with the accounts this login owns and the command to rerun for each, so the
// fix is a copy-paste rather than a trip to the console.
func errWorkspaceNeedsAccount(ctx context.Context, client *cliauth.BackendClient, token string, cause error, opts workspaceCreateOpts) error {
	var b strings.Builder
	fmt.Fprintf(&b, "create workspace: %v\n\n", cause)
	b.WriteString("  this login owns more than one account; name the one the workspace belongs to with --account.\n")
	accounts := ownedAccounts(ctx, client, token, cause)
	if len(accounts) == 0 {
		fmt.Fprintf(&b, "  rerun: %s\n", workspaceCreateCommandLine(opts, "<ws_…|slug>"))
		b.WriteString("  (account ids are in the console's account settings; `orun workspace list` shows the account each workspace belongs to)")
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}
	headers := []string{"ACCOUNT", "ACCOUNT ID", "NAME"}
	rows := make([][]string, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, []string{orDash(a.Slug), orDash(a.ID), orDash(a.Name)})
	}
	b.WriteString("  accounts you own:\n")
	b.WriteString(indentBlock(indentBlock(renderColumns(headers, rows))))
	b.WriteString("  rerun with one of:\n")
	for _, a := range accounts {
		fmt.Fprintf(&b, "    %s\n", workspaceCreateCommandLine(opts, a.Label()))
	}
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// workspaceCreateCommandLine spells the create command as invoked, with the
// account filled in: the rerun hint and nothing else prints it.
func workspaceCreateCommandLine(opts workspaceCreateOpts, account string) string {
	path := strings.TrimSpace(opts.CommandPath)
	if path == "" {
		path = "orun workspace create"
	}
	parts := []string{path, shellQuote(opts.Name)}
	if s := strings.TrimSpace(opts.Slug); s != "" {
		parts = append(parts, "--slug", shellQuote(s))
	}
	if strings.HasPrefix(account, "<") {
		// A placeholder for the reader to fill in, not a value to quote.
		parts = append(parts, "--account", account)
	} else {
		parts = append(parts, "--account", shellQuote(account))
	}
	return strings.Join(parts, " ")
}

// accountDisplay renders "slug (id)" when both are known, else whichever
// identifier there is; "" when neither.
func accountDisplay(id, slug string) string {
	id, slug = strings.TrimSpace(id), strings.TrimSpace(slug)
	switch {
	case slug != "" && id != "" && slug != id:
		return fmt.Sprintf("%s (%s)", slug, id)
	case slug != "":
		return slug
	default:
		return id
	}
}

// withOrgAccounts fills in the owning account of each workspace for
// `orun workspace list`. The session's own list carries it when the login
// payload does; otherwise the backend's organizations listing is asked once,
// and account ids are turned into slugs through the accounts listing. Every
// step is best effort: a backend that reports no accounts leaves the list as
// it was, and the column is then not shown at all.
func withOrgAccounts(ctx context.Context, orgs []orgChoice) []orgChoice {
	if len(orgs) == 0 {
		return orgs
	}
	ctx, cancel := context.WithTimeout(ctx, accountListTimeout)
	defer cancel()
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
	if err != nil {
		return orgs
	}
	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return orgs
	}
	client := cliauth.NewBackendClient(backendURL, version)
	if !anyOrgHasAccount(orgs) {
		listed, err := client.ListMyOrgs(ctx, token)
		if err != nil {
			return orgs
		}
		for i := range orgs {
			for _, l := range listed {
				if l.AccountID != "" && (orgs[i].matches(l.WorkspaceRef) || orgs[i].matches(l.ID)) {
					orgs[i].AccountID, orgs[i].AccountSlug = l.AccountID, l.AccountSlug
					break
				}
			}
		}
	}
	needSlug := false
	for _, o := range orgs {
		if o.AccountID != "" && o.AccountSlug == "" {
			needSlug = true
		}
	}
	if !needSlug {
		return orgs
	}
	accounts, err := client.ListMyAccounts(ctx, token)
	if err != nil {
		return orgs
	}
	for i := range orgs {
		if orgs[i].AccountSlug != "" {
			continue
		}
		for _, a := range accounts {
			if a.Matches(orgs[i].AccountID) {
				orgs[i].AccountSlug = a.Slug
				break
			}
		}
	}
	return orgs
}

func anyOrgHasAccount(orgs []orgChoice) bool {
	for _, o := range orgs {
		if strings.TrimSpace(o.AccountID) != "" {
			return true
		}
	}
	return false
}
