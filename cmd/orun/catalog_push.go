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
	cmd.Flags().StringVar(&catalogPushOrg, "org", "", "Org id/slug (overrides the cached link)")
	cmd.Flags().StringVar(&catalogPushProject, "project", "", "Project id/slug (overrides the cached link)")
	cmd.Flags().StringVar(&catalogPushEnvironment, "environment", "", "Target a named environment head (default: the project-wide head)")
	parent.AddCommand(cmd)
}

func runCatalogPush() error {
	ctx := context.Background()
	backendURL, err := requireBackendURL(nil, catalogPushBackendURL)
	if err != nil {
		return err
	}

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
	scope := resolveScope(catalogPushOrg, catalogPushProject, linkOrg, linkProject)
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
	head, previous, err := client.AdvanceCatalogHead(ctx, rootDigest, catalogPushEnvironment, commit)
	if err != nil {
		return err
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s pushed catalog %s\n", ui.Green(color, "✓"), rootDigest)
	fmt.Printf("  objects: %d in closure, %d uploaded, %d already present\n", res.Closure, res.Copied, res.Skipped)
	scopeLabel := "project-wide head"
	if strings.TrimSpace(catalogPushEnvironment) != "" {
		scopeLabel = catalogPushEnvironment + " head"
	}
	if previous != nil && strings.TrimSpace(previous.Digest) != "" {
		fmt.Printf("  %s: %s → %s\n", scopeLabel, previous.Digest, head.Digest)
	} else {
		fmt.Printf("  %s: → %s (first publish)\n", scopeLabel, head.Digest)
	}
	return nil
}
