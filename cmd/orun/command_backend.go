package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/backendbundle"
	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/cloudflare"
	"github.com/spf13/cobra"
)

const (
	cfAccountIDEnvVar  = "CLOUDFLARE_ACCOUNT_ID"
	cfAPITokenEnvVar   = "CLOUDFLARE_API_TOKEN"
	orunSessionSecretEnvVar = "ORUN_SESSION_SECRET"
	ghClientIDEnvVar   = "GITHUB_CLIENT_ID"
	ghClientSecretEnvVar = "GITHUB_CLIENT_SECRET"
	orunDashboardURLEnvVar = "ORUN_DASHBOARD_URL"
	managedByValue     = "orun-backend-init"
)

var (
	backendCmd = &cobra.Command{
		Use:   "backend",
		Short: "Provision and manage a self-hosted Orun backend on Cloudflare",
	}

	// shared flags
	backendAccountID  string
	backendAPIToken   string

	// init flags
	initName           string
	initD1Name         string
	initR2Bucket       string
	initOIDCAudience   string
	initPublicURL      string
	initDashboardURL   string
	initGitHubClientID string
	initGitHubClientSecret string
	initSessionSecret  string
	initDryRun         bool
	initJSON           bool

	// status flags
	backendStatusJSON bool
	backendStatusName string

	// destroy flags
	destroyName    string
	destroyYes     bool
	destroyDryRun  bool
	destroyJSON    bool
	destroyAdopted bool
)

func registerBackendCommand(root *cobra.Command) {
	root.AddCommand(backendCmd)
	backendCmd.PersistentFlags().StringVar(&backendAccountID, "account-id", "", fmt.Sprintf("Cloudflare account ID (or set %s)", cfAccountIDEnvVar))
	backendCmd.PersistentFlags().StringVar(&backendAPIToken, "api-token", "", fmt.Sprintf("Cloudflare API token (or set %s)", cfAPITokenEnvVar))

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Provision a self-hosted Orun backend on Cloudflare",
		Long: `Provision the Orun backend on your Cloudflare account.

Creates (or reuses) a D1 database, R2 bucket, and Cloudflare Worker script,
applies database migrations, configures Worker bindings and vars, and optionally
sets secrets. Running init twice is safe — existing resources are reused.

A GitHub OAuth app is required for dashboard and CLI authentication.
Run without GitHub OAuth flags to provision the API-only backend first, then
set GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET and re-run init to finish auth setup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendInit(cmd.Context())
		},
	}
	initCmd.Flags().StringVar(&initName, "name", "orun-api", "Worker script name")
	initCmd.Flags().StringVar(&initD1Name, "d1-name", "orun-db", "D1 database name")
	initCmd.Flags().StringVar(&initR2Bucket, "r2-bucket", "orun-storage", "R2 bucket name")
	initCmd.Flags().StringVar(&initOIDCAudience, "oidc-audience", "orun", "GitHub OIDC audience (GITHUB_OIDC_AUDIENCE Worker var)")
	initCmd.Flags().StringVar(&initPublicURL, "public-url", "", "Public URL for the Worker (ORUN_PUBLIC_URL); inferred from workers.dev if omitted")
	initCmd.Flags().StringVar(&initDashboardURL, "dashboard-url", "", "Dashboard URL (ORUN_DASHBOARD_URL; optional)")
	initCmd.Flags().StringVar(&initGitHubClientID, "github-client-id", "", "GitHub OAuth app client ID")
	initCmd.Flags().StringVar(&initGitHubClientSecret, "github-client-secret", "", "GitHub OAuth app client secret (not stored in config)")
	initCmd.Flags().StringVar(&initSessionSecret, "session-secret", "", "Orun session HMAC secret (generated securely if absent)")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "Print planned actions without touching Cloudflare")
	initCmd.Flags().BoolVar(&initJSON, "json", false, "Output machine-readable JSON")
	backendCmd.AddCommand(initCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check Orun backend resource readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendStatus(cmd.Context())
		},
	}
	statusCmd.Flags().StringVar(&backendStatusName, "name", "", "Worker script name (defaults to stored bootstrap value or orun-api)")
	statusCmd.Flags().BoolVar(&backendStatusJSON, "json", false, "Output machine-readable JSON")
	backendCmd.AddCommand(statusCmd)

	destroyCmd := &cobra.Command{
		Use:   "destroy",
		Short: "Remove Orun backend resources from Cloudflare (DESTRUCTIVE)",
		Long: `Remove all Orun backend resources managed by orun backend init.

This will permanently delete the Cloudflare Worker script, D1 database, and R2 bucket.
D1 and R2 data cannot be recovered after deletion.

By default, destroy only operates on resources recorded by orun backend init.
Use --adopted to also destroy resources by name that were not created by this CLI.

Requires --yes for non-interactive execution.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendDestroy(cmd.Context())
		},
	}
	destroyCmd.Flags().StringVar(&destroyName, "name", "", "Worker script name (defaults to stored bootstrap value)")
	destroyCmd.Flags().BoolVar(&destroyYes, "yes", false, "Confirm destructive deletion")
	destroyCmd.Flags().BoolVar(&destroyDryRun, "dry-run", false, "Show destruction plan without deleting resources")
	destroyCmd.Flags().BoolVar(&destroyJSON, "json", false, "Output machine-readable JSON")
	destroyCmd.Flags().BoolVar(&destroyAdopted, "adopted", false, "Allow destroying resources not recorded by orun backend init")
	backendCmd.AddCommand(destroyCmd)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func resolveBackendCreds() (accountID, apiToken string, err error) {
	accountID = backendAccountID
	if accountID == "" {
		accountID = os.Getenv(cfAccountIDEnvVar)
	}
	apiToken = backendAPIToken
	if apiToken == "" {
		apiToken = os.Getenv(cfAPITokenEnvVar)
	}
	if accountID == "" || apiToken == "" {
		return "", "", fmt.Errorf("Cloudflare credentials required; set %s and %s, or use --account-id and --api-token", cfAccountIDEnvVar, cfAPITokenEnvVar)
	}
	return accountID, apiToken, nil
}

func newCFClient(userAgent string) (*cloudflare.Client, string, string, error) {
	accountID, apiToken, err := resolveBackendCreds()
	if err != nil {
		return nil, "", "", err
	}
	client := cloudflare.New(cloudflare.Options{
		AccountID: accountID,
		APIToken:  apiToken,
		UserAgent: userAgent,
	})
	return client, accountID, apiToken, nil
}

func generateSessionSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ── backend init ──────────────────────────────────────────────────────────────

type initResult struct {
	DryRun         bool     `json:"dryRun"`
	WorkerName     string   `json:"workerName"`
	D1DatabaseName string   `json:"d1DatabaseName"`
	D1DatabaseUUID string   `json:"d1DatabaseUUID,omitempty"`
	R2BucketName   string   `json:"r2BucketName"`
	BackendURL     string   `json:"backendUrl,omitempty"`
	MigrationsApplied int  `json:"migrationsApplied"`
	Warnings       []string `json:"warnings,omitempty"`
}

func runBackendInit(ctx context.Context) error {
	manifest, err := backendbundle.GetManifest()
	if err != nil {
		return fmt.Errorf("read bundle manifest: %w", err)
	}
	migrations, err := backendbundle.Migrations()
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	workerBundle := backendbundle.WorkerBundle()

	workerName := initName
	d1Name := initD1Name
	r2Name := initR2Bucket
	audience := initOIDCAudience
	publicURL := strings.TrimSpace(initPublicURL)
	dashboardURL := strings.TrimSpace(initDashboardURL)
	if dashboardURL == "" {
		dashboardURL = os.Getenv(orunDashboardURLEnvVar)
	}
	ghClientID := strings.TrimSpace(initGitHubClientID)
	if ghClientID == "" {
		ghClientID = os.Getenv(ghClientIDEnvVar)
	}
	ghClientSecret := strings.TrimSpace(initGitHubClientSecret)
	if ghClientSecret == "" {
		ghClientSecret = os.Getenv(ghClientSecretEnvVar)
	}
	sessionSecret := strings.TrimSpace(initSessionSecret)
	if sessionSecret == "" {
		sessionSecret = os.Getenv(orunSessionSecretEnvVar)
	}

	result := initResult{
		DryRun:         initDryRun,
		WorkerName:     workerName,
		D1DatabaseName: d1Name,
		R2BucketName:   r2Name,
	}

	if initDryRun {
		if !initJSON {
			fmt.Fprintf(os.Stdout, "[dry-run] Would provision:\n")
			fmt.Fprintf(os.Stdout, "  D1 database:   %s\n", d1Name)
			fmt.Fprintf(os.Stdout, "  R2 bucket:     %s\n", r2Name)
			fmt.Fprintf(os.Stdout, "  Worker script: %s\n", workerName)
			fmt.Fprintf(os.Stdout, "  Migrations:    %d\n", len(migrations))
			fmt.Fprintf(os.Stdout, "  Worker vars:   GITHUB_JWKS_URL, GITHUB_OIDC_AUDIENCE")
			if publicURL != "" {
				fmt.Fprintf(os.Stdout, ", ORUN_PUBLIC_URL")
			}
			if dashboardURL != "" {
				fmt.Fprintf(os.Stdout, ", ORUN_DASHBOARD_URL")
			}
			fmt.Fprintln(os.Stdout)
			fmt.Fprintf(os.Stdout, "  Worker secrets: ORUN_SESSION_SECRET")
			if ghClientID != "" {
				fmt.Fprintf(os.Stdout, ", GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET")
			}
			fmt.Fprintln(os.Stdout)
			fmt.Fprintf(os.Stdout, "  Bundle commit: %s\n", manifest.BackendCommitSHA)
		}
		if initJSON {
			return printJSON(result)
		}
		return nil
	}

	ua := "orun-cli/" + version
	client, accountID, _, err := newCFClient(ua)
	if err != nil {
		return err
	}

	// 1. Create or reuse D1 database.
	fmt.Fprintf(os.Stdout, "Provisioning D1 database %q...\n", d1Name)
	db, err := client.CreateD1Database(ctx, d1Name)
	if err != nil {
		return fmt.Errorf("provision D1: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  D1 UUID: %s\n", db.UUID)
	result.D1DatabaseUUID = db.UUID

	// 2. Apply migrations in order using an orun bootstrap ledger.
	fmt.Fprintf(os.Stdout, "Applying %d migrations...\n", len(migrations))
	applied, err := applyMigrations(ctx, client, db.UUID, migrations)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %d migration(s) applied (skipped already-applied).\n", applied)
	result.MigrationsApplied = applied

	// 3. Create or reuse R2 bucket.
	fmt.Fprintf(os.Stdout, "Provisioning R2 bucket %q...\n", r2Name)
	_, err = client.CreateR2Bucket(ctx, r2Name)
	if err != nil {
		return fmt.Errorf("provision R2: %w", err)
	}
	fmt.Fprintln(os.Stdout, "  R2 bucket ready.")

	// 4. Upload Worker script with bindings.
	fmt.Fprintf(os.Stdout, "Uploading Worker script %q...\n", workerName)
	bindings := []cloudflare.WorkerBinding{
		{Type: "durable_object_namespace", Name: "COORDINATOR", ClassName: "RunCoordinator", ScriptName: workerName},
		{Type: "durable_object_namespace", Name: "RATE_LIMITER", ClassName: "RateLimitCounter", ScriptName: workerName},
		{Type: "d1", Name: "DB", DatabaseID: db.UUID},
		{Type: "r2_bucket", Name: "STORAGE", BucketName: r2Name},
	}
	doMigrations := []cloudflare.DurableObjectMigration{
		{Tag: "v1", NewSQLiteClasses: []string{"RunCoordinator", "RateLimitCounter"}},
	}
	_, err = client.UploadWorkerScript(ctx, cloudflare.UploadWorkerParams{
		ScriptName:         workerName,
		Bundle:             workerBundle,
		Bindings:           bindings,
		DOMiddleMigrations: doMigrations,
		CompatDate:         "2024-01-01",
	})
	if err != nil {
		return fmt.Errorf("upload Worker: %w", err)
	}
	fmt.Fprintln(os.Stdout, "  Worker uploaded.")

	// 5. Enable workers.dev route and discover Worker URL.
	_ = client.EnableWorkerSubdomainRoute(ctx, workerName) // best-effort
	if publicURL == "" {
		subdomain, subErr := client.GetWorkerSubdomain(ctx)
		if subErr == nil && subdomain != "" {
			publicURL = fmt.Sprintf("https://%s.%s.workers.dev", workerName, subdomain)
		}
	}
	result.BackendURL = publicURL

	// 6. Set Worker vars.
	vars := map[string]string{
		"GITHUB_JWKS_URL":    "https://token.actions.githubusercontent.com/.well-known/jwks",
		"GITHUB_OIDC_AUDIENCE": audience,
	}
	if publicURL != "" {
		vars["ORUN_PUBLIC_URL"] = publicURL
	}
	if dashboardURL != "" {
		vars["ORUN_DASHBOARD_URL"] = dashboardURL
	}
	fmt.Fprintln(os.Stdout, "Setting Worker vars...")
	if err := client.SetWorkerVars(ctx, workerName, vars); err != nil {
		return fmt.Errorf("set Worker vars: %w", err)
	}

	// 7. Set Worker secrets.
	if sessionSecret == "" {
		generated, genErr := generateSessionSecret()
		if genErr != nil {
			return genErr
		}
		sessionSecret = generated
		fmt.Fprintln(os.Stdout, "  Generated ORUN_SESSION_SECRET (not stored in config).")
	}
	fmt.Fprintln(os.Stdout, "Setting Worker secrets...")
	if err := client.SetWorkerSecret(ctx, workerName, "ORUN_SESSION_SECRET", sessionSecret); err != nil {
		return fmt.Errorf("set ORUN_SESSION_SECRET: %w", err)
	}
	if ghClientID != "" && ghClientSecret != "" {
		if err := client.SetWorkerSecret(ctx, workerName, "GITHUB_CLIENT_ID", ghClientID); err != nil {
			return fmt.Errorf("set GITHUB_CLIENT_ID: %w", err)
		}
		if err := client.SetWorkerSecret(ctx, workerName, "GITHUB_CLIENT_SECRET", ghClientSecret); err != nil {
			return fmt.Errorf("set GITHUB_CLIENT_SECRET: %w", err)
		}
	} else {
		result.Warnings = append(result.Warnings, "GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET not set: dashboard/CLI OAuth will not work until a GitHub OAuth app is configured and orun backend init is re-run with --github-client-id and --github-client-secret")
	}

	// 8. Save non-secret bootstrap metadata.
	meta := cliauth.BackendBootstrap{
		ManagedBy:      managedByValue,
		AccountID:      accountID,
		WorkerName:     workerName,
		D1DatabaseName: d1Name,
		D1DatabaseUUID: db.UUID,
		R2BucketName:   r2Name,
		BackendCommit:  manifest.BackendCommitSHA,
		InitAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := cliauth.SaveBootstrapMetadata(meta, publicURL); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save bootstrap metadata: %v\n", err)
	}

	// 9. Print summary.
	if initJSON {
		return printJSON(result)
	}
	fmt.Fprintln(os.Stdout, "\n✓ Orun backend provisioned successfully.")
	fmt.Fprintf(os.Stdout, "  Worker:       %s\n", workerName)
	fmt.Fprintf(os.Stdout, "  D1 database:  %s (%s)\n", d1Name, db.UUID)
	fmt.Fprintf(os.Stdout, "  R2 bucket:    %s\n", r2Name)
	if publicURL != "" {
		fmt.Fprintf(os.Stdout, "  Backend URL:  %s\n", publicURL)
	} else {
		fmt.Fprintln(os.Stdout, "  Backend URL:  (unknown — pass --public-url or configure a custom domain)")
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if publicURL != "" {
		fmt.Fprintf(os.Stdout, "\nNext: orun auth login --backend-url %s\n", publicURL)
	}
	return nil
}

// applyMigrations applies D1 migrations that have not yet been recorded in the
// orun bootstrap ledger. The ledger is a simple table created on first use.
func applyMigrations(ctx context.Context, client *cloudflare.Client, dbUUID string, migrations []backendbundle.Migration) (int, error) {
	// Ensure the orun bootstrap ledger table exists.
	const createLedger = `CREATE TABLE IF NOT EXISTS _orun_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := client.ExecD1SQL(ctx, dbUUID, createLedger); err != nil {
		return 0, fmt.Errorf("create migration ledger: %w", err)
	}

	applied := 0
	for _, m := range migrations {
		// Check if this migration is already applied.
		checkSQL := fmt.Sprintf(`SELECT name FROM _orun_migrations WHERE name = '%s'`, strings.ReplaceAll(m.Name, "'", "''"))
		result, err := client.ExecD1SQL(ctx, dbUUID, checkSQL)
		if err != nil {
			return applied, fmt.Errorf("check migration %s: %w", m.Name, err)
		}
		if len(result.Results) > 0 {
			continue // already applied
		}

		// Apply the migration.
		if _, err := client.ExecD1SQL(ctx, dbUUID, m.SQL); err != nil {
			return applied, fmt.Errorf("apply migration %s: %w", m.Name, err)
		}
		// Record it in the ledger.
		recordSQL := fmt.Sprintf(`INSERT INTO _orun_migrations (name, applied_at) VALUES ('%s', '%s')`,
			strings.ReplaceAll(m.Name, "'", "''"),
			time.Now().UTC().Format(time.RFC3339),
		)
		if _, err := client.ExecD1SQL(ctx, dbUUID, recordSQL); err != nil {
			return applied, fmt.Errorf("record migration %s: %w", m.Name, err)
		}
		applied++
		fmt.Fprintf(os.Stdout, "  Applied: %s\n", m.Name)
	}
	return applied, nil
}

// ── backend status ────────────────────────────────────────────────────────────

type statusResult struct {
	WorkerReady      bool     `json:"workerReady"`
	WorkerName       string   `json:"workerName"`
	D1Ready          bool     `json:"d1Ready"`
	D1DatabaseName   string   `json:"d1DatabaseName"`
	D1DatabaseUUID   string   `json:"d1DatabaseUUID,omitempty"`
	R2Ready          bool     `json:"r2Ready"`
	R2BucketName     string   `json:"r2BucketName"`
	MigrationsReady  bool     `json:"migrationsReady"`
	SecretsConfigured []string `json:"secretsConfigured"`
	BackendURL        string   `json:"backendUrl,omitempty"`
	Issues           []string `json:"issues,omitempty"`
}

func runBackendStatus(ctx context.Context) error {
	meta, err := cliauth.LoadBootstrapMetadata()
	if err != nil {
		return fmt.Errorf("load bootstrap metadata: %w", err)
	}

	workerName := backendStatusName
	if workerName == "" && meta != nil {
		workerName = meta.WorkerName
	}
	if workerName == "" {
		workerName = "orun-api"
	}

	ua := "orun-cli/" + version
	client, _, _, err := newCFClient(ua)
	if err != nil {
		return err
	}

	result := statusResult{
		WorkerName:     workerName,
		D1DatabaseName: "orun-db",
		R2BucketName:   "orun-storage",
	}
	if meta != nil {
		result.D1DatabaseName = meta.D1DatabaseName
		result.D1DatabaseUUID = meta.D1DatabaseUUID
		result.R2BucketName = meta.R2BucketName
	}

	// Check Worker.
	script, err := client.GetWorkerScript(ctx, workerName)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("worker check failed: %v", err))
	} else {
		result.WorkerReady = script != nil
		if !result.WorkerReady {
			result.Issues = append(result.Issues, fmt.Sprintf("Worker script %q not found", workerName))
		}
	}

	// Check D1.
	db, err := client.FindD1DatabaseByName(ctx, result.D1DatabaseName)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("D1 check failed: %v", err))
	} else {
		result.D1Ready = db != nil
		if db != nil {
			result.D1DatabaseUUID = db.UUID
		} else {
			result.Issues = append(result.Issues, fmt.Sprintf("D1 database %q not found", result.D1DatabaseName))
		}
	}

	// Check migrations.
	if result.D1Ready && result.D1DatabaseUUID != "" {
		migrations, _ := backendbundle.Migrations()
		appliedCount, checkErr := countAppliedMigrations(ctx, client, result.D1DatabaseUUID)
		if checkErr == nil {
			result.MigrationsReady = appliedCount >= len(migrations)
			if !result.MigrationsReady {
				result.Issues = append(result.Issues, fmt.Sprintf("migrations: %d/%d applied", appliedCount, len(migrations)))
			}
		}
	}

	// Check R2.
	bucket, err := client.FindR2BucketByName(ctx, result.R2BucketName)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("R2 check failed: %v", err))
	} else {
		result.R2Ready = bucket != nil
		if !result.R2Ready {
			result.Issues = append(result.Issues, fmt.Sprintf("R2 bucket %q not found", result.R2BucketName))
		}
	}

	// Check secrets (names only, not values).
	secretNames, err := client.ListWorkerSecretNames(ctx, workerName)
	if err == nil {
		result.SecretsConfigured = secretNames
	}

	// Backend URL.
	if meta != nil {
		cfg, cfgErr := cliauth.LoadConfig()
		if cfgErr == nil && cfg != nil {
			result.BackendURL = cfg.Backend.URL
		}
	}

	if backendStatusJSON {
		return printJSON(result)
	}

	ok := result.WorkerReady && result.D1Ready && result.R2Ready && result.MigrationsReady
	fmt.Fprintf(os.Stdout, "Worker (%s):   %s\n", result.WorkerName, readyStr(result.WorkerReady))
	fmt.Fprintf(os.Stdout, "D1 database:       %s\n", readyStr(result.D1Ready))
	fmt.Fprintf(os.Stdout, "R2 bucket:         %s\n", readyStr(result.R2Ready))
	fmt.Fprintf(os.Stdout, "Migrations:        %s\n", readyStr(result.MigrationsReady))
	fmt.Fprintf(os.Stdout, "Secrets set:       %s\n", strings.Join(result.SecretsConfigured, ", "))
	if result.BackendURL != "" {
		fmt.Fprintf(os.Stdout, "Backend URL:       %s\n", result.BackendURL)
	}
	for _, issue := range result.Issues {
		fmt.Fprintf(os.Stderr, "  issue: %s\n", issue)
	}
	if !ok {
		return fmt.Errorf("backend is not fully ready; run `orun backend init` to provision missing resources")
	}
	return nil
}

func countAppliedMigrations(ctx context.Context, client *cloudflare.Client, dbUUID string) (int, error) {
	result, err := client.ExecD1SQL(ctx, dbUUID, "SELECT COUNT(*) as cnt FROM _orun_migrations")
	if err != nil {
		return 0, err
	}
	if len(result.Results) > 0 {
		if cnt, ok := result.Results[0]["cnt"]; ok {
			switch v := cnt.(type) {
			case float64:
				return int(v), nil
			}
		}
	}
	return 0, nil
}

func readyStr(ok bool) string {
	if ok {
		return "ready"
	}
	return "NOT READY"
}

// ── backend destroy ───────────────────────────────────────────────────────────

type destroyResult struct {
	DryRun         bool   `json:"dryRun"`
	WorkerDeleted  bool   `json:"workerDeleted"`
	D1Deleted      bool   `json:"d1Deleted"`
	R2Deleted      bool   `json:"r2Deleted"`
	WorkerName     string `json:"workerName"`
	D1DatabaseUUID string `json:"d1DatabaseUUID,omitempty"`
	R2BucketName   string `json:"r2BucketName"`
}

func runBackendDestroy(ctx context.Context) error {
	// Dry-run is always safe, no --yes required.
	if destroyDryRun {
		meta, _ := cliauth.LoadBootstrapMetadata()
		workerName := destroyName
		d1UUID := ""
		r2Name := "orun-storage"
		if meta != nil {
			if workerName == "" {
				workerName = meta.WorkerName
			}
			d1UUID = meta.D1DatabaseUUID
			r2Name = meta.R2BucketName
		}
		if workerName == "" {
			workerName = "orun-api"
		}
		result := destroyResult{
			DryRun:         true,
			WorkerName:     workerName,
			D1DatabaseUUID: d1UUID,
			R2BucketName:   r2Name,
		}
		if !destroyJSON {
			fmt.Fprintf(os.Stdout, "[dry-run] Would destroy:\n")
			fmt.Fprintf(os.Stdout, "  Worker script: %s\n", workerName)
			if meta != nil && d1UUID != "" {
				fmt.Fprintf(os.Stdout, "  D1 database:   %s (%s)\n", meta.D1DatabaseName, d1UUID)
			}
			fmt.Fprintf(os.Stdout, "  R2 bucket:     %s\n", r2Name)
			fmt.Fprintln(os.Stdout, "  WARNING: D1 and R2 data deletion is irreversible.")
		}
		if destroyJSON {
			return printJSON(result)
		}
		return nil
	}

	// Require --yes for actual destruction.
	if !destroyYes {
		return fmt.Errorf("destroy is destructive and irreversible; re-run with --yes to confirm, or use --dry-run to preview")
	}

	meta, err := cliauth.LoadBootstrapMetadata()
	if err != nil {
		return fmt.Errorf("load bootstrap metadata: %w", err)
	}

	// Guard: require managed metadata unless --adopted is set.
	if !destroyAdopted && (meta == nil || meta.ManagedBy != managedByValue) {
		return fmt.Errorf("no bootstrap metadata found; run `orun backend init` first, or pass --adopted to destroy resources by name without managed-resource safeguards")
	}

	workerName := destroyName
	d1UUID := ""
	r2Name := ""
	if meta != nil {
		if workerName == "" {
			workerName = meta.WorkerName
		}
		d1UUID = meta.D1DatabaseUUID
		r2Name = meta.R2BucketName
	}
	if workerName == "" {
		workerName = "orun-api"
	}
	if r2Name == "" {
		r2Name = "orun-storage"
	}

	result := destroyResult{
		DryRun:         false,
		WorkerName:     workerName,
		D1DatabaseUUID: d1UUID,
		R2BucketName:   r2Name,
	}

	ua := "orun-cli/" + version
	client, _, _, err := newCFClient(ua)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "WARNING: Deleting Orun backend resources. D1 and R2 data cannot be recovered.")

	// Delete Worker first (it references D1/R2 bindings).
	fmt.Fprintf(os.Stdout, "Deleting Worker %q...\n", workerName)
	if err := client.DeleteWorkerScript(ctx, workerName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: delete Worker: %v\n", err)
	} else {
		result.WorkerDeleted = true
	}

	// Delete D1 database.
	if d1UUID != "" {
		fmt.Fprintf(os.Stdout, "Deleting D1 database %s...\n", d1UUID)
		if err := client.DeleteD1Database(ctx, d1UUID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: delete D1: %v\n", err)
		} else {
			result.D1Deleted = true
		}
	}

	// Delete R2 bucket.
	fmt.Fprintf(os.Stdout, "Deleting R2 bucket %q...\n", r2Name)
	if err := client.DeleteR2Bucket(ctx, r2Name); err != nil {
		fmt.Fprintf(os.Stderr, "warning: delete R2: %v\n", err)
	} else {
		result.R2Deleted = true
	}

	// Clear local bootstrap metadata.
	_ = cliauth.ClearBootstrapMetadata()

	if destroyJSON {
		return printJSON(result)
	}
	fmt.Fprintln(os.Stdout, "Backend resources destroyed.")
	fmt.Fprintln(os.Stdout, "Note: GitHub OAuth app configuration was not changed.")
	return nil
}

// ── utility ───────────────────────────────────────────────────────────────────

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
