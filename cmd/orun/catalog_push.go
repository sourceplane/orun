package main

// `orun catalog push` (OCv2-3; specs/orun-cloud/cli-surface.md). Sync the
// locally resolved catalog snapshot to Orun Cloud and advance the catalog head,
// recording the source commit — which lights up the org-global catalog browser
// (orun-cloud OV6) within seconds.
//
// Two steps, in order (the head advance fails closed if the closure is not
// uploaded first):
//   1. objremote.Sync copies the catalogs/current closure as a SET DIFFERENCE —
//      only the blobs the server lacks — then moves the remote catalogs/current
//      ref. Content addressing makes a re-push transfer ~zero bytes.
//   2. AdvanceCatalogHead points the (project, environment) head at the snapshot
//      digest with the git commit; the server projects it into the org graph.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/objremote"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

const catalogCurrentRef = "catalogs/current"

var (
	catalogPushBackendURL  string
	catalogPushOrg         string
	catalogPushProject     string
	catalogPushEnvironment string
)

func registerCatalogPushCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Sync the resolved catalog snapshot to Orun Cloud and advance the head",
		Long: `Sync the resolved catalog snapshot to Orun Cloud and advance the head.

Uploads the snapshot's object closure (only the blobs the server is missing) and
advances the catalog head to it, recording the source git commit. The org-global
catalog browser reflects the new snapshot within seconds.

Run ` + "`orun catalog refresh`" + ` first to (re)resolve the workspace; push
uploads whatever 'catalogs/current' points at. Use --environment to target a
named environment head instead of the project-wide head.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogPush()
		},
	}
	cmd.Flags().StringVar(&catalogPushBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().StringVar(&catalogPushOrg, "workspace", "", "Workspace slug/id (overrides the cached link)")
	cmd.Flags().StringVar(&catalogPushOrg, "org", "", "Alias of --workspace (legacy spelling)")
	cmd.Flags().StringVar(&catalogPushProject, "project", "", "Project id/slug (overrides the cached link)")
	cmd.Flags().StringVar(&catalogPushEnvironment, "environment", "", "Target a named environment head (default: the project-wide head)")
	parent.AddCommand(cmd)
}

// validatePushToken eagerly resolves a session/static token before any object
// sync. ResolveTokenSource only constructs a SessionTokenSource from whatever
// session file is present — it does not prove a token can be minted — so a
// logged-out or expired/revoked session otherwise slips through and surfaces as
// a raw token error from deep inside objremote.Sync (after a refresh
// round-trip's delay), and only deterministically as the actionable login
// message on the *next* run once the stale session is cleared. Resolving here
// maps the failure to the clean message on every run and skips the wasted sync.
// (OIDC sources are exchanged + validated by the caller, so they skip this.)
func validatePushToken(ctx context.Context, tokenSrc remotestate.TokenSource) error {
	if _, err := tokenSrc.Token(ctx); err != nil {
		if isNoLoginErr(err) {
			return errNotLoggedIn()
		}
		return fmt.Errorf("remote state auth: %w", err)
	}
	return nil
}

func runCatalogPush() error {
	ctx := context.Background()
	backendURL, err := requireBackendURL(nil, catalogPushBackendURL)
	if err != nil {
		return err
	}
	return pushResolvedCatalog(ctx, backendURL, catalogPushOrg, catalogPushProject, catalogPushEnvironment)
}

// pushResolvedCatalog uploads the local catalogs/current closure to the backend
// and advances the (project, environment) catalog head. Shared by `catalog push`
// and `catalog refresh --push`; the caller resolves backendURL (so each command
// honors its own --backend-url), and org/project may be "" to fall back to env +
// the cached link (or the OIDC-resolved scope in CI).
func pushResolvedCatalog(ctx context.Context, backendURL, orgFlag, projectFlag, environment string) error {
	// Open the local object-model store + the catalogs/current ref it resolved.
	store, refs, _, err := openObjectModel()
	if err != nil {
		return fmt.Errorf("open local object model: %w", err)
	}
	localRef, err := refs.Read(ctx, catalogCurrentRef)
	if err != nil {
		return fmt.Errorf("no local catalog to push (%s); run `orun catalog refresh` first", catalogCurrentRef)
	}
	rootDigest := localRef.Target

	// Resolve scope (flag > env > cached link) and a token (session or CI OIDC).
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	linkOrg, linkProject := "", ""
	if repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	// Scope precedence flag > env > intent > cached link (specs/oidc-ci-tenancy
	// §4.1). The declared org is sent on the OIDC exchange / API-key request so
	// the server can enforce claim ⊆ authorized.
	intentOrg, intentProject, requireOrg := intentScope(loadIntentForCloudConfig())
	scope := resolveScope(orgFlag, projectFlag, intentOrg, intentProject, linkOrg, linkProject)
	if !termIsInteractive() {
		if err := enforceRequireOrg(requireOrg, scope.OrgID); err != nil {
			return err
		}
	}
	if scope.OrgID == "" && scope.ProjectID == "" && !isOSSBackend(backendURL) {
		return errRepoNotLinked(backendURL)
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
			return errNotLoggedIn()
		}
		return fmt.Errorf("remote state auth: %w", err)
	}
	// Credential-agnostic CI (OCv2-2): adopt the OIDC-resolved scope where empty.
	if oidcSrc, ok := tokenSrc.(*remotestate.OIDCTokenSource); ok {
		if _, terr := oidcSrc.Token(ctx); terr != nil {
			return fmt.Errorf("remote state auth (oidc exchange): %w", terr)
		}
		exOrg, exProject := oidcSrc.ResolvedScope()
		if scope.OrgID == "" {
			scope.OrgID = exOrg
		}
		if scope.ProjectID == "" {
			scope.ProjectID = exProject
		}
	} else if err := validatePushToken(ctx, tokenSrc); err != nil {
		return err
	}
	client := remotestate.NewClientWithScope(backendURL, version, tokenSrc, scope)
	remoteStore, remoteRefs := client.RemoteStores()

	// Step 1 — copy the closure (set difference) + move the remote ref.
	res, err := objremote.Sync(ctx,
		objremote.Endpoint{Objects: store, Refs: refs},
		objremote.Endpoint{Objects: remoteStore, Refs: remoteRefs},
		catalogCurrentRef,
	)
	if err != nil {
		return fmt.Errorf("sync catalog objects: %w", err)
	}

	// Step 2 — advance the catalog head (the org-global projection trigger).
	commit, _, _ := gitProvenanceForRun(ctx, storeDir())
	head, previous, err := client.AdvanceCatalogHead(ctx, rootDigest, environment, commit)
	if err != nil {
		return err
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s pushed catalog %s\n", ui.Green(color, "✓"), rootDigest)
	fmt.Printf("  objects: %d in closure, %d uploaded, %d already present\n", res.Closure, res.Copied, res.Skipped)
	scopeLabel := "project-wide head"
	if strings.TrimSpace(environment) != "" {
		scopeLabel = environment + " head"
	}
	if previous != nil && strings.TrimSpace(previous.Digest) != "" {
		fmt.Printf("  %s: %s → %s\n", scopeLabel, previous.Digest, head.Digest)
	} else {
		fmt.Printf("  %s: → %s (first publish)\n", scopeLabel, head.Digest)
	}
	return nil
}
