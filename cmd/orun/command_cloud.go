package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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
		Short: "Link the current GitHub repo to the local Orun config",
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
	creds, err := cliauth.LoadSession()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("not logged in; run `orun auth login` first")
		}
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || repo.RepoFullName == "" {
		return fmt.Errorf("could not detect a GitHub remote for this workspace")
	}
	if repo.NamespaceID == "" {
		linked, listErr := cliauth.NewBackendClient(backendURL, version).ListLinkedRepos(context.Background(), creds.AccessToken)
		if listErr != nil {
			return fmt.Errorf("list linked repos: %w", listErr)
		}
		for _, candidate := range linked {
			if strings.EqualFold(candidate.NamespaceSlug, repo.RepoFullName) {
				repo.NamespaceID = candidate.NamespaceID
				break
			}
		}
	}
	if repo.NamespaceID == "" {
		return fmt.Errorf("repo %s is not linked in this Orun session; link it in Orun Cloud first, then rerun `orun cloud link`", repo.RepoFullName)
	}
	if err := persistRepoLink(backendURL, repo, repo.NamespaceID); err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s linked %s\n", ui.Green(color, "✓"), repo.RepoFullName)
	fmt.Printf("  backend: %s\n", backendURL)
	fmt.Printf("  namespace: %s\n", repo.NamespaceID)
	return nil
}
