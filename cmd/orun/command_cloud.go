package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var cloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Manage Orun Cloud workspace linkage",
}

func registerCloudCommand(root *cobra.Command) {
	root.AddCommand(cloudCmd)
	cloudCmd.AddCommand(&cobra.Command{
		Use:   "link",
		Short: "Link the current GitHub repo to the local Orun config via the active CLI session",
		Long: `Link the current GitHub repo to the local Orun config.

Detects the git remote, resolves the repo namespace through the Orun backend
CLI session endpoint, and persists the namespace ID in ~/.orun/config.yaml.

No GitHub PAT or OAuth token is required — the Orun CLI session established
by 'orun auth login' is sufficient.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudLink()
		},
	})
}

func runCloudLink() error {
	backendURL, err := requireBackendURL(nil, authBackendURL)
	if err != nil {
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || repo.RepoFullName == "" {
		return fmt.Errorf("could not detect a GitHub remote for this workspace; local remote-state requires a GitHub remote")
	}

	creds, err := cliauth.LoadSession()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("not logged in; run `orun auth login` first")
		}
		return err
	}

	// Refresh access token if expired.
	exp := creds.AccessExpiryTime()
	if creds.AccessToken == "" || (!exp.IsZero() && time.Now().After(exp)) {
		creds, err = cliauth.RefreshSession(context.Background(), backendURL, version, creds)
		if err != nil {
			return fmt.Errorf("refresh login: %w; run `orun auth login` again", err)
		}
	}

	client := cliauth.NewBackendClient(backendURL, version)
	linked, err := client.LinkRepoFromSession(context.Background(), creds.AccessToken, repo.RepoFullName)
	if err != nil {
		return translateLinkError(err, repo.RepoFullName)
	}

	if err := persistRepoLink(backendURL, repo, linked.NamespaceID); err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s linked %s\n", ui.Green(color, "✓"), repo.RepoFullName)
	fmt.Printf("  backend:   %s\n", backendURL)
	fmt.Printf("  namespace: %s\n", linked.NamespaceID)
	return nil
}
