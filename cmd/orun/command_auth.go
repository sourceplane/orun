package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	authBackendURL string
	authDevice     bool
	authAudience   string
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Orun CLI authentication",
}

func registerAuthCommand(root *cobra.Command) {
	root.AddCommand(authCmd)
	authCmd.PersistentFlags().StringVar(&authBackendURL, "backend-url", "", "orun-backend URL for auth and remote state")

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate this CLI with Orun",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin()
		},
	}
	loginCmd.Flags().BoolVar(&authDevice, "device", false, "Use backend-mediated GitHub device login")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show current Orun CLI login status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus()
		},
	}

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the current Orun CLI login",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogout()
		},
	}

	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Print a short-lived Orun access token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthToken()
		},
	}
	tokenCmd.Flags().StringVar(&authAudience, "audience", "", "Audience label for the requested token (display-only for CLI sessions)")

	authCmd.AddCommand(loginCmd, statusCmd, logoutCmd, tokenCmd)
}

func runAuthLogin() error {
	backendURL, err := requireBackendURL(nil, authBackendURL)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var creds *cliauth.Credentials
	if authDevice {
		creds, err = cliauth.DeviceLogin(ctx, backendURL, version, os.Stdout)
	} else {
		creds, err = cliauth.BrowserLogin(ctx, backendURL, version, os.Stdout, nil)
	}
	if err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s logged in as %s\n", ui.Green(color, "✓"), creds.GitHubLogin)
	fmt.Printf("  backend: %s\n", backendURL)
	if exp := creds.AccessExpiryTime(); !exp.IsZero() {
		fmt.Printf("  access token expires: %s\n", exp.Format(time.RFC3339))
	}
	return nil
}

func runAuthStatus() error {
	backendURL, _ := requireBackendURL(nil, authBackendURL)
	creds, err := cliauth.LoadSession()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("not logged in; run `orun auth login` or `orun auth login --device`")
		}
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	resolvedBackend := backendURL
	if resolvedBackend == "" {
		resolvedBackend = creds.BackendURL
	}
	fmt.Printf("GitHub login: %s\n", valueOrUnknown(creds.GitHubLogin))
	fmt.Printf("Backend URL: %s\n", valueOrUnknown(resolvedBackend))
	if exp := creds.AccessExpiryTime(); !exp.IsZero() {
		state := "valid"
		if time.Now().After(exp) {
			state = "expired"
		}
		fmt.Printf("Access token: %s (%s)\n", exp.Format(time.RFC3339), state)
	}
	repo, repoErr := resolveRepoContext(resolvedBackend)
	if repoErr == nil && repo != nil && repo.RepoFullName != "" {
		status := ui.Yellow(color, "not linked")
		if repo.NamespaceID != "" {
			status = ui.Green(color, "linked")
		}
		fmt.Printf("Current Git remote: %s (%s)\n", repo.RepoFullName, status)
	}
	return nil
}

func runAuthLogout() error {
	creds, err := cliauth.LoadSession()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if creds != nil && strings.TrimSpace(creds.RefreshToken) != "" {
		backendURL, backendErr := requireBackendURL(nil, authBackendURL)
		if backendErr == nil {
			client := cliauth.NewBackendClient(backendURL, version)
			_ = client.Logout(context.Background(), creds.RefreshToken)
		}
	}
	if err := cliauth.ClearSession(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s logged out\n", ui.Green(color, "✓"))
	return nil
}

func runAuthToken() error {
	backendURL, err := requireBackendURL(nil, authBackendURL)
	if err != nil {
		return err
	}
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  true,
		RequireLogin: true,
	})
	if err != nil {
		return err
	}
	token, err := tokenSrc.Token(context.Background())
	if err != nil {
		return err
	}
	_ = authAudience
	fmt.Println(token)
	return nil
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unknown)"
	}
	return value
}
