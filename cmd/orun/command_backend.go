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
	cfAccountIDEnvVar       = "CLOUDFLARE_ACCOUNT_ID"
	cfAPITokenEnvVar        = "CLOUDFLARE_API_TOKEN"
	orunSessionSecretEnvVar = "ORUN_SESSION_SECRET"
	ghClientIDEnvVar        = "GITHUB_CLIENT_ID"
	ghClientSecretEnvVar    = "GITHUB_CLIENT_SECRET"
	orunDashboardURLEnvVar  = "ORUN_DASHBOARD_URL"
	managedByValue          = "orun-backend-init"

	defaultCatalogQueue = "orun-catalog-ingest"
	defaultCatalogDLQ   = "orun-catalog-ingest-dlq"
	defaultCatalogCron  = "*/15 * * * *"
)

var (
	backendCmd = &cobra.Command{
		Use:   "backend",
		Short: "Provision and manage a self-hosted Orun backend on Cloudflare",
	}

	// shared flags
	backendAccountID string
	backendAPIToken  string

	// init flags
	initName               string
	initD1Name             string
	initR2Bucket           string
	initCatalogQueue       string
	initCatalogDLQ         string
	initCatalogCron        string
	initOIDCAudience       string
	initPublicURL          string
	initDashboardURL       string
	initGitHubClientID     string
	initGitHubClientSecret string
	initSessionSecret      string
	initDryRun             bool
	initJSON               bool

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

Creates (or reuses) a D1 database, R2 bucket, Cloudflare Worker, catalog queue,
DLQ, queue consumer, and cron trigger. Applies database migrations, configures
Worker bindings and vars, and optionally sets secrets. Running init twice is
safe — existing resources are reused idempotently.

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
	initCmd.Flags().StringVar(&initCatalogQueue, "catalog-queue", defaultCatalogQueue, "Catalog ingest queue name")
	initCmd.Flags().StringVar(&initCatalogDLQ, "catalog-dlq", defaultCatalogDLQ, "Catalog ingest dead-letter queue name")
	initCmd.Flags().StringVar(&initCatalogCron, "catalog-cron", defaultCatalogCron, "Cron schedule for Worker scheduled tasks")
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

This will permanently delete the Cloudflare Worker script, D1 database, R2 bucket,
catalog queue, DLQ, consumer, and cron schedule. D1 and R2 data cannot be recovered.

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
	DryRun            bool     `json:"dryRun"`
	WorkerName        string   `json:"workerName"`
	D1DatabaseName    string   `json:"d1DatabaseName"`
	D1DatabaseUUID    string   `json:"d1DatabaseUUID,omitempty"`
	R2BucketName      string   `json:"r2BucketName"`
	CatalogQueueName  string   `json:"catalogQueueName"`
	CatalogDLQName    string   `json:"catalogDLQName"`
	CatalogCron       string   `json:"catalogCron"`
	BackendURL        string   `json:"backendUrl,omitempty"`
	MigrationsApplied int      `json:"migrationsApplied"`
	MigrationCount    int      `json:"migrationCount"`
	Warnings          []string `json:"warnings,omitempty"`
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
	catalogQueue := initCatalogQueue
	catalogDLQ := initCatalogDLQ
	catalogCron := initCatalogCron
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
		DryRun:           initDryRun,
		WorkerName:       workerName,
		D1DatabaseName:   d1Name,
		R2BucketName:     r2Name,
		CatalogQueueName: catalogQueue,
		CatalogDLQName:   catalogDLQ,
		CatalogCron:      catalogCron,
		MigrationCount:   len(migrations),
	}

	if initDryRun {
		if !initJSON {
			fmt.Fprintf(os.Stdout, "[dry-run] Would provision:\n")
			fmt.Fprintf(os.Stdout, "  D1 database:    %s\n", d1Name)
			fmt.Fprintf(os.Stdout, "  R2 bucket:      %s\n", r2Name)
			fmt.Fprintf(os.Stdout, "  Worker script:  %s\n", workerName)
			fmt.Fprintf(os.Stdout, "  Migrations:     %d\n", len(migrations))
			fmt.Fprintf(os.Stdout, "  Catalog queue:  %s\n", catalogQueue)
			fmt.Fprintf(os.Stdout, "  Catalog DLQ:    %s\n", catalogDLQ)
			fmt.Fprintf(os.Stdout, "  Queue consumer: %s (batch_size=10, max_retries=3, max_wait_ms=30000, dlq=%s)\n", workerName, catalogDLQ)
			fmt.Fprintf(os.Stdout, "  Cron schedule:  %s\n", catalogCron)
			fmt.Fprintf(os.Stdout, "  Worker vars:    GITHUB_JWKS_URL, GITHUB_OIDC_AUDIENCE")
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
			fmt.Fprintf(os.Stdout, "  Bundle commit:  %s\n", manifest.BackendCommitSHA)
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

	// 4. Create or reuse catalog queue.
	fmt.Fprintf(os.Stdout, "Provisioning catalog queue %q...\n", catalogQueue)
	q, err := client.CreateQueue(ctx, catalogQueue)
	if err != nil {
		return fmt.Errorf("provision catalog queue: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  Queue ID: %s\n", q.QueueID)

	// 5. Create or reuse catalog DLQ.
	fmt.Fprintf(os.Stdout, "Provisioning catalog DLQ %q...\n", catalogDLQ)
	dlq, err := client.CreateQueue(ctx, catalogDLQ)
	if err != nil {
		return fmt.Errorf("provision catalog DLQ: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  DLQ ID: %s\n", dlq.QueueID)

	// 6. Upload Worker script with all bindings (DO, D1, R2, queue, vars) in one call.
	// Including vars as plain_text bindings avoids the PATCH /settings clobber risk.
	fmt.Fprintf(os.Stdout, "Uploading Worker script %q...\n", workerName)
	bindings := []cloudflare.WorkerBinding{
		{Type: "durable_object_namespace", Name: "COORDINATOR", ClassName: "RunCoordinator", ScriptName: workerName},
		{Type: "durable_object_namespace", Name: "RATE_LIMITER", ClassName: "RateLimitCounter", ScriptName: workerName},
		{Type: "d1", Name: "DB", DatabaseID: db.UUID},
		{Type: "r2_bucket", Name: "STORAGE", BucketName: r2Name},
		{Type: "queue", Name: "CATALOG_INGEST_QUEUE", QueueName: catalogQueue},
		{Type: "plain_text", Name: "GITHUB_JWKS_URL", Text: "https://token.actions.githubusercontent.com/.well-known/jwks"},
		{Type: "plain_text", Name: "GITHUB_OIDC_AUDIENCE", Text: audience},
	}
	if publicURL != "" {
		bindings = append(bindings, cloudflare.WorkerBinding{Type: "plain_text", Name: "ORUN_PUBLIC_URL", Text: publicURL})
	}
	if dashboardURL != "" {
		bindings = append(bindings, cloudflare.WorkerBinding{Type: "plain_text", Name: "ORUN_DASHBOARD_URL", Text: dashboardURL})
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

	// 7. Enable workers.dev route and discover Worker URL.
	_ = client.EnableWorkerSubdomainRoute(ctx, workerName) // best-effort
	if publicURL == "" {
		subdomain, subErr := client.GetWorkerSubdomain(ctx)
		if subErr == nil && subdomain != "" {
			publicURL = fmt.Sprintf("https://%s.%s.workers.dev", workerName, subdomain)
		}
	}
	result.BackendURL = publicURL

	// 8. Set Worker secrets.
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

	// 9. Attach queue consumer.
	fmt.Fprintf(os.Stdout, "Attaching queue consumer to %q...\n", catalogQueue)
	consumerSettings := cloudflare.QueueConsumerSettings{
		BatchSize:     10,
		MaxRetries:    3,
		MaxWaitTimeMs: 30000,
	}
	if err := client.CreateOrUpdateQueueConsumer(ctx, q.QueueID, workerName, catalogDLQ, consumerSettings); err != nil {
		return fmt.Errorf("attach queue consumer: %w", err)
	}
	fmt.Fprintln(os.Stdout, "  Queue consumer attached.")

	// 10. Set cron schedule.
	fmt.Fprintf(os.Stdout, "Setting cron schedule %q...\n", catalogCron)
	if err := client.UpdateWorkerSchedules(ctx, workerName, []string{catalogCron}); err != nil {
		return fmt.Errorf("set cron schedule: %w", err)
	}
	fmt.Fprintln(os.Stdout, "  Cron schedule set.")

	// 11. Save non-secret bootstrap metadata.
	meta := cliauth.BackendBootstrap{
		ManagedBy:        managedByValue,
		AccountID:        accountID,
		WorkerName:       workerName,
		D1DatabaseName:   d1Name,
		D1DatabaseUUID:   db.UUID,
		R2BucketName:     r2Name,
		CatalogQueueName: catalogQueue,
		CatalogQueueID:   q.QueueID,
		CatalogDLQName:   catalogDLQ,
		CatalogDLQID:     dlq.QueueID,
		CatalogCron:      catalogCron,
		BackendCommit:    manifest.BackendCommitSHA,
		InitAt:           time.Now().UTC().Format(time.RFC3339),
	}
	if err := cliauth.SaveBootstrapMetadata(meta, publicURL); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save bootstrap metadata: %v\n", err)
	}

	// 12. Print summary.
	if initJSON {
		return printJSON(result)
	}
	fmt.Fprintln(os.Stdout, "\n✓ Orun backend provisioned successfully.")
	fmt.Fprintf(os.Stdout, "  Worker:         %s\n", workerName)
	fmt.Fprintf(os.Stdout, "  D1 database:    %s (%s)\n", d1Name, db.UUID)
	fmt.Fprintf(os.Stdout, "  R2 bucket:      %s\n", r2Name)
	fmt.Fprintf(os.Stdout, "  Catalog queue:  %s\n", catalogQueue)
	fmt.Fprintf(os.Stdout, "  Catalog DLQ:    %s\n", catalogDLQ)
	fmt.Fprintf(os.Stdout, "  Cron schedule:  %s\n", catalogCron)
	if publicURL != "" {
		fmt.Fprintf(os.Stdout, "  Backend URL:    %s\n", publicURL)
	} else {
		fmt.Fprintln(os.Stdout, "  Backend URL:    (unknown — pass --public-url or configure a custom domain)")
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
	const createLedger = `CREATE TABLE IF NOT EXISTS _orun_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := client.ExecD1SQL(ctx, dbUUID, createLedger); err != nil {
		return 0, fmt.Errorf("create migration ledger: %w", err)
	}

	applied := 0
	for _, m := range migrations {
		checkSQL := fmt.Sprintf(`SELECT name FROM _orun_migrations WHERE name = '%s'`, strings.ReplaceAll(m.Name, "'", "''"))
		result, err := client.ExecD1SQL(ctx, dbUUID, checkSQL)
		if err != nil {
			return applied, fmt.Errorf("check migration %s: %w", m.Name, err)
		}
		if len(result.Results) > 0 {
			continue
		}

		if _, err := client.ExecD1SQL(ctx, dbUUID, m.SQL); err != nil {
			return applied, fmt.Errorf("apply migration %s: %w", m.Name, err)
		}
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

type consumerStatusSummary struct {
	BatchSize     int    `json:"batchSize"`
	MaxRetries    int    `json:"maxRetries"`
	MaxWaitTimeMs int    `json:"maxWaitTimeMs"`
	DLQ           string `json:"dlq"`
}

type statusResult struct {
	WorkerReady       bool                   `json:"workerReady"`
	WorkerName        string                 `json:"workerName"`
	D1Ready           bool                   `json:"d1Ready"`
	D1DatabaseName    string                 `json:"d1DatabaseName"`
	D1DatabaseUUID    string                 `json:"d1DatabaseUUID,omitempty"`
	R2Ready           bool                   `json:"r2Ready"`
	R2BucketName      string                 `json:"r2BucketName"`
	MigrationsReady   bool                   `json:"migrationsReady"`
	CatalogQueueReady bool                   `json:"catalogQueueReady"`
	CatalogQueueName  string                 `json:"catalogQueueName"`
	CatalogDLQReady   bool                   `json:"catalogDLQReady"`
	CatalogDLQName    string                 `json:"catalogDLQName"`
	ConsumerReady     bool                   `json:"consumerReady"`
	ConsumerSettings  *consumerStatusSummary `json:"consumerSettings,omitempty"`
	CronReady         bool                   `json:"cronReady"`
	CronSchedule      string                 `json:"cronSchedule,omitempty"`
	SecretsConfigured []string               `json:"secretsConfigured"`
	BackendURL        string                 `json:"backendUrl,omitempty"`
	Issues            []string               `json:"issues,omitempty"`
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
		WorkerName:       workerName,
		D1DatabaseName:   "orun-db",
		R2BucketName:     "orun-storage",
		CatalogQueueName: defaultCatalogQueue,
		CatalogDLQName:   defaultCatalogDLQ,
	}
	if meta != nil {
		result.D1DatabaseName = meta.D1DatabaseName
		result.D1DatabaseUUID = meta.D1DatabaseUUID
		result.R2BucketName = meta.R2BucketName
		if meta.CatalogQueueName != "" {
			result.CatalogQueueName = meta.CatalogQueueName
		}
		if meta.CatalogDLQName != "" {
			result.CatalogDLQName = meta.CatalogDLQName
		}
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

	// Check catalog queue.
	catalogQ, err := client.FindQueueByName(ctx, result.CatalogQueueName)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("catalog queue check failed: %v", err))
	} else {
		result.CatalogQueueReady = catalogQ != nil
		if !result.CatalogQueueReady {
			result.Issues = append(result.Issues, fmt.Sprintf("catalog queue %q not found", result.CatalogQueueName))
		}
	}

	// Check catalog DLQ.
	dlqQ, err := client.FindQueueByName(ctx, result.CatalogDLQName)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("catalog DLQ check failed: %v", err))
	} else {
		result.CatalogDLQReady = dlqQ != nil
		if !result.CatalogDLQReady {
			result.Issues = append(result.Issues, fmt.Sprintf("catalog DLQ %q not found", result.CatalogDLQName))
		}
	}

	// Check queue consumer settings.
	if result.CatalogQueueReady && catalogQ != nil {
		consumers, consErr := client.ListQueueConsumers(ctx, catalogQ.QueueID)
		if consErr == nil {
			for _, c := range consumers {
				if c.ScriptName == workerName && c.Type == "worker" {
					result.ConsumerReady = true
					result.ConsumerSettings = &consumerStatusSummary{
						BatchSize:     c.Settings.BatchSize,
						MaxRetries:    c.Settings.MaxRetries,
						MaxWaitTimeMs: c.Settings.MaxWaitTimeMs,
						DLQ:           c.DeadLetterQueue,
					}
					break
				}
			}
			if !result.ConsumerReady {
				result.Issues = append(result.Issues, fmt.Sprintf("no Worker consumer for %q found on catalog queue", workerName))
			}
		} else {
			result.Issues = append(result.Issues, fmt.Sprintf("consumer check failed: %v", consErr))
		}
	}

	// Check cron schedule.
	schedules, schedErr := client.ListWorkerSchedules(ctx, workerName)
	if schedErr == nil {
		expectedCron := defaultCatalogCron
		if meta != nil && meta.CatalogCron != "" {
			expectedCron = meta.CatalogCron
		}
		for _, s := range schedules {
			if s.Cron == expectedCron {
				result.CronReady = true
				result.CronSchedule = s.Cron
				break
			}
		}
		if !result.CronReady {
			if len(schedules) > 0 {
				result.CronSchedule = schedules[0].Cron
				result.Issues = append(result.Issues, fmt.Sprintf("cron schedule %q not found (found: %s)", expectedCron, result.CronSchedule))
			} else {
				result.Issues = append(result.Issues, "no cron schedule configured")
			}
		}
	} else {
		result.Issues = append(result.Issues, fmt.Sprintf("cron check failed: %v", schedErr))
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

	ok := result.WorkerReady && result.D1Ready && result.R2Ready && result.MigrationsReady &&
		result.CatalogQueueReady && result.CatalogDLQReady && result.ConsumerReady && result.CronReady
	fmt.Fprintf(os.Stdout, "Worker (%s):       %s\n", result.WorkerName, readyStr(result.WorkerReady))
	fmt.Fprintf(os.Stdout, "D1 database:           %s\n", readyStr(result.D1Ready))
	fmt.Fprintf(os.Stdout, "R2 bucket:             %s\n", readyStr(result.R2Ready))
	fmt.Fprintf(os.Stdout, "Migrations:            %s\n", readyStr(result.MigrationsReady))
	fmt.Fprintf(os.Stdout, "Catalog queue (%s): %s\n", result.CatalogQueueName, readyStr(result.CatalogQueueReady))
	fmt.Fprintf(os.Stdout, "Catalog DLQ (%s): %s\n", result.CatalogDLQName, readyStr(result.CatalogDLQReady))
	fmt.Fprintf(os.Stdout, "Queue consumer:        %s\n", readyStr(result.ConsumerReady))
	fmt.Fprintf(os.Stdout, "Cron schedule:         %s\n", readyStr(result.CronReady))
	if result.CronSchedule != "" {
		fmt.Fprintf(os.Stdout, "  Schedule: %s\n", result.CronSchedule)
	}
	fmt.Fprintf(os.Stdout, "Secrets set:           %s\n", strings.Join(result.SecretsConfigured, ", "))
	if result.BackendURL != "" {
		fmt.Fprintf(os.Stdout, "Backend URL:           %s\n", result.BackendURL)
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
	DryRun              bool   `json:"dryRun"`
	WorkerDeleted       bool   `json:"workerDeleted"`
	D1Deleted           bool   `json:"d1Deleted"`
	R2Deleted           bool   `json:"r2Deleted"`
	ConsumerDeleted     bool   `json:"consumerDeleted"`
	CatalogQueueDeleted bool   `json:"catalogQueueDeleted"`
	CatalogDLQDeleted   bool   `json:"catalogDLQDeleted"`
	CronCleared         bool   `json:"cronCleared"`
	WorkerName          string `json:"workerName"`
	D1DatabaseUUID      string `json:"d1DatabaseUUID,omitempty"`
	R2BucketName        string `json:"r2BucketName"`
	CatalogQueueName    string `json:"catalogQueueName,omitempty"`
	CatalogDLQName      string `json:"catalogDLQName,omitempty"`
}

func runBackendDestroy(ctx context.Context) error {
	if destroyDryRun {
		meta, _ := cliauth.LoadBootstrapMetadata()
		workerName := destroyName
		d1UUID := ""
		r2Name := "orun-storage"
		catalogQueue := defaultCatalogQueue
		catalogDLQ := defaultCatalogDLQ
		catalogCron := defaultCatalogCron
		if meta != nil {
			if workerName == "" {
				workerName = meta.WorkerName
			}
			d1UUID = meta.D1DatabaseUUID
			r2Name = meta.R2BucketName
			if meta.CatalogQueueName != "" {
				catalogQueue = meta.CatalogQueueName
			}
			if meta.CatalogDLQName != "" {
				catalogDLQ = meta.CatalogDLQName
			}
			if meta.CatalogCron != "" {
				catalogCron = meta.CatalogCron
			}
		}
		if workerName == "" {
			workerName = "orun-api"
		}
		result := destroyResult{
			DryRun:           true,
			WorkerName:       workerName,
			D1DatabaseUUID:   d1UUID,
			R2BucketName:     r2Name,
			CatalogQueueName: catalogQueue,
			CatalogDLQName:   catalogDLQ,
		}
		if !destroyJSON {
			fmt.Fprintf(os.Stdout, "[dry-run] Would destroy:\n")
			fmt.Fprintf(os.Stdout, "  Worker script:  %s\n", workerName)
			fmt.Fprintf(os.Stdout, "  Cron schedule:  %s (cleared)\n", catalogCron)
			fmt.Fprintf(os.Stdout, "  Queue consumer: %s on %s\n", workerName, catalogQueue)
			fmt.Fprintf(os.Stdout, "  Catalog queue:  %s\n", catalogQueue)
			fmt.Fprintf(os.Stdout, "  Catalog DLQ:    %s\n", catalogDLQ)
			if meta != nil && d1UUID != "" {
				fmt.Fprintf(os.Stdout, "  D1 database:    %s (%s)\n", meta.D1DatabaseName, d1UUID)
			}
			fmt.Fprintf(os.Stdout, "  R2 bucket:      %s\n", r2Name)
			fmt.Fprintln(os.Stdout, "  WARNING: D1 and R2 data deletion is irreversible.")
		}
		if destroyJSON {
			return printJSON(result)
		}
		return nil
	}

	if !destroyYes {
		return fmt.Errorf("destroy is destructive and irreversible; re-run with --yes to confirm, or use --dry-run to preview")
	}

	meta, err := cliauth.LoadBootstrapMetadata()
	if err != nil {
		return fmt.Errorf("load bootstrap metadata: %w", err)
	}

	if !destroyAdopted && (meta == nil || meta.ManagedBy != managedByValue) {
		return fmt.Errorf("no bootstrap metadata found; run `orun backend init` first, or pass --adopted to destroy resources by name without managed-resource safeguards")
	}

	workerName := destroyName
	d1UUID := ""
	r2Name := ""
	catalogQueue := ""
	catalogQueueID := ""
	catalogDLQ := ""
	if meta != nil {
		if workerName == "" {
			workerName = meta.WorkerName
		}
		d1UUID = meta.D1DatabaseUUID
		r2Name = meta.R2BucketName
		catalogQueue = meta.CatalogQueueName
		catalogQueueID = meta.CatalogQueueID
		catalogDLQ = meta.CatalogDLQName
	}
	if workerName == "" {
		workerName = "orun-api"
	}
	if r2Name == "" {
		r2Name = "orun-storage"
	}

	result := destroyResult{
		DryRun:           false,
		WorkerName:       workerName,
		D1DatabaseUUID:   d1UUID,
		R2BucketName:     r2Name,
		CatalogQueueName: catalogQueue,
		CatalogDLQName:   catalogDLQ,
	}

	ua := "orun-cli/" + version
	client, _, _, err := newCFClient(ua)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "WARNING: Deleting Orun backend resources. D1 and R2 data cannot be recovered.")

	// 1. Clear cron schedule first (Worker still exists, so this is safe).
	fmt.Fprintf(os.Stdout, "Clearing cron schedule for %q...\n", workerName)
	if err := client.DeleteWorkerSchedules(ctx, workerName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clear cron schedule: %v\n", err)
	} else {
		result.CronCleared = true
	}

	// 2. Delete queue consumer (before queue, to avoid dangling binding errors).
	if catalogQueue != "" && catalogQueueID != "" {
		fmt.Fprintf(os.Stdout, "Removing queue consumer from %q...\n", catalogQueue)
		consumers, listErr := client.ListQueueConsumers(ctx, catalogQueueID)
		if listErr == nil {
			for _, c := range consumers {
				if c.ScriptName == workerName && c.Type == "worker" {
					if delErr := client.DeleteQueueConsumer(ctx, catalogQueueID, c.ConsumerID); delErr != nil {
						fmt.Fprintf(os.Stderr, "warning: delete queue consumer: %v\n", delErr)
					} else {
						result.ConsumerDeleted = true
					}
					break
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: list consumers: %v\n", listErr)
		}
	}

	// 3. Delete Worker (references D1/R2/queue bindings).
	fmt.Fprintf(os.Stdout, "Deleting Worker %q...\n", workerName)
	if err := client.DeleteWorkerScript(ctx, workerName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: delete Worker: %v\n", err)
	} else {
		result.WorkerDeleted = true
	}

	// 4. Delete catalog queue.
	if catalogQueue != "" {
		fmt.Fprintf(os.Stdout, "Deleting catalog queue %q...\n", catalogQueue)
		if err := client.DeleteQueueByName(ctx, catalogQueue); err != nil {
			fmt.Fprintf(os.Stderr, "warning: delete catalog queue: %v\n", err)
		} else {
			result.CatalogQueueDeleted = true
		}
	}

	// 5. Delete catalog DLQ.
	if catalogDLQ != "" {
		fmt.Fprintf(os.Stdout, "Deleting catalog DLQ %q...\n", catalogDLQ)
		if err := client.DeleteQueueByName(ctx, catalogDLQ); err != nil {
			fmt.Fprintf(os.Stderr, "warning: delete catalog DLQ: %v\n", err)
		} else {
			result.CatalogDLQDeleted = true
		}
	}

	// 6. Delete D1 database.
	if d1UUID != "" {
		fmt.Fprintf(os.Stdout, "Deleting D1 database %s...\n", d1UUID)
		if err := client.DeleteD1Database(ctx, d1UUID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: delete D1: %v\n", err)
		} else {
			result.D1Deleted = true
		}
	}

	// 7. Delete R2 bucket.
	fmt.Fprintf(os.Stdout, "Deleting R2 bucket %q...\n", r2Name)
	if err := client.DeleteR2Bucket(ctx, r2Name); err != nil {
		fmt.Fprintf(os.Stderr, "warning: delete R2: %v\n", err)
	} else {
		result.R2Deleted = true
	}

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
