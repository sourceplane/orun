// Package cloudflare provides a focused Cloudflare REST API client for
// provisioning the resources required by the Orun backend Worker.
//
// It supports D1 database create/list/delete, R2 bucket create/list/delete,
// Worker script upload with bindings, Worker vars/secrets, and status checks.
//
// Credentials are read from CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID
// environment variables by default, and can be overridden via Options.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	defaultBaseURL    = "https://api.cloudflare.com/client/v4"
	defaultTimeout    = 30 * time.Second
	uploadTimeout     = 120 * time.Second
)

// Client is a Cloudflare REST API client.
type Client struct {
	accountID  string
	apiToken   string
	baseURL    string
	userAgent  string
	httpClient *http.Client
	// uploadClient uses a longer timeout for script upload operations.
	uploadClient *http.Client
}

// Options configures the Cloudflare client.
type Options struct {
	AccountID  string
	APIToken   string
	BaseURL    string // defaults to https://api.cloudflare.com/client/v4
	UserAgent  string
	HTTPClient *http.Client // injectable for tests
}

// New creates a new Cloudflare API client.
func New(opts Options) *Client {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "orun-cli/dev"
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	uploadClient := &http.Client{
		Timeout:   uploadTimeout,
		Transport: httpClient.Transport,
	}
	return &Client{
		accountID:    opts.AccountID,
		apiToken:     opts.APIToken,
		baseURL:      base,
		userAgent:    ua,
		httpClient:   httpClient,
		uploadClient: uploadClient,
	}
}

// APIError represents a Cloudflare API error.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cloudflare error %d: %s", e.Code, e.Message)
}

// apiEnvelope is the standard Cloudflare API response wrapper.
type apiEnvelope struct {
	Success  bool             `json:"success"`
	Errors   []APIError       `json:"errors"`
	Messages []struct{ Text string } `json:"messages"`
	Result   json.RawMessage  `json:"result"`
}

// ── D1 ───────────────────────────────────────────────────────────────────────

// D1Database represents a Cloudflare D1 database.
type D1Database struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListD1Databases returns all D1 databases for the account.
func (c *Client) ListD1Databases(ctx context.Context) ([]D1Database, error) {
	var dbs []D1Database
	if err := c.doJSON(ctx, http.MethodGet, c.accountPath("/d1/database?per_page=100"), nil, &dbs); err != nil {
		return nil, fmt.Errorf("list D1 databases: %w", err)
	}
	return dbs, nil
}

// FindD1DatabaseByName returns the D1 database with the given name, or nil if not found.
func (c *Client) FindD1DatabaseByName(ctx context.Context, name string) (*D1Database, error) {
	dbs, err := c.ListD1Databases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range dbs {
		if strings.EqualFold(dbs[i].Name, name) {
			return &dbs[i], nil
		}
	}
	return nil, nil
}

// CreateD1Database creates a new D1 database and returns it.
// If a database with the given name already exists, it is returned without error.
func (c *Client) CreateD1Database(ctx context.Context, name string) (*D1Database, error) {
	existing, err := c.FindD1DatabaseByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	body := map[string]string{"name": name}
	var db D1Database
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/d1/database"), body, &db); err != nil {
		return nil, fmt.Errorf("create D1 database %q: %w", name, err)
	}
	return &db, nil
}

// DeleteD1Database deletes a D1 database by UUID.
func (c *Client) DeleteD1Database(ctx context.Context, databaseUUID string) error {
	if err := c.doJSON(ctx, http.MethodDelete, c.accountPath("/d1/database/"+databaseUUID), nil, nil); err != nil {
		return fmt.Errorf("delete D1 database %s: %w", databaseUUID, err)
	}
	return nil
}

// D1QueryRequest is the body for D1 query execution.
type D1QueryRequest struct {
	SQL    string   `json:"sql"`
	Params []string `json:"params,omitempty"`
}

// D1QueryResult holds the result of a D1 SQL query.
type D1QueryResult struct {
	Results []map[string]interface{} `json:"results"`
	Success bool                     `json:"success"`
	Meta    struct {
		Duration float64 `json:"duration"`
	} `json:"meta"`
}

// ExecD1SQL executes a SQL statement against a D1 database.
func (c *Client) ExecD1SQL(ctx context.Context, databaseUUID, sql string) (*D1QueryResult, error) {
	body := []D1QueryRequest{{SQL: sql}}
	path := c.accountPath("/d1/database/" + databaseUUID + "/query")
	var results []D1QueryResult
	if err := c.doJSON(ctx, http.MethodPost, path, body, &results); err != nil {
		return nil, fmt.Errorf("exec D1 SQL: %w", err)
	}
	if len(results) == 0 {
		return &D1QueryResult{Success: true}, nil
	}
	return &results[0], nil
}

// ── R2 ───────────────────────────────────────────────────────────────────────

// R2Bucket represents a Cloudflare R2 bucket.
type R2Bucket struct {
	Name         string `json:"name"`
	CreationDate string `json:"creation_date"`
}

// ListR2Buckets returns all R2 buckets for the account.
func (c *Client) ListR2Buckets(ctx context.Context) ([]R2Bucket, error) {
	type listResult struct {
		Buckets []R2Bucket `json:"buckets"`
	}
	var result listResult
	if err := c.doJSON(ctx, http.MethodGet, c.accountPath("/r2/buckets"), nil, &result); err != nil {
		return nil, fmt.Errorf("list R2 buckets: %w", err)
	}
	return result.Buckets, nil
}

// FindR2BucketByName returns the R2 bucket with the given name, or nil if not found.
func (c *Client) FindR2BucketByName(ctx context.Context, name string) (*R2Bucket, error) {
	buckets, err := c.ListR2Buckets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range buckets {
		if buckets[i].Name == name {
			return &buckets[i], nil
		}
	}
	return nil, nil
}

// CreateR2Bucket creates a new R2 bucket.
// If a bucket with the given name already exists, it is returned without error.
func (c *Client) CreateR2Bucket(ctx context.Context, name string) (*R2Bucket, error) {
	existing, err := c.FindR2BucketByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	body := map[string]string{"name": name}
	var bucket R2Bucket
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/r2/buckets"), body, &bucket); err != nil {
		return nil, fmt.Errorf("create R2 bucket %q: %w", name, err)
	}
	return &bucket, nil
}

// DeleteR2Bucket deletes an R2 bucket by name.
func (c *Client) DeleteR2Bucket(ctx context.Context, name string) error {
	if err := c.doJSON(ctx, http.MethodDelete, c.accountPath("/r2/buckets/"+name), nil, nil); err != nil {
		return fmt.Errorf("delete R2 bucket %q: %w", name, err)
	}
	return nil
}

// ── Workers ──────────────────────────────────────────────────────────────────

// WorkerScript represents a deployed Worker script.
type WorkerScript struct {
	ID         string `json:"id"`
	ETAG       string `json:"etag"`
	Handlers   []string `json:"handlers"`
	ModifiedOn string `json:"modified_on"`
	CreatedOn  string `json:"created_on"`
}

// WorkerBinding represents a Worker binding declaration.
type WorkerBinding struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	ScriptName string `json:"script_name,omitempty"`
	ClassName  string `json:"class_name,omitempty"`
	DatabaseID string `json:"id,omitempty"`
	BucketName string `json:"bucket_name,omitempty"`
	QueueName  string `json:"queue_name,omitempty"`
	Text       string `json:"text,omitempty"`
}

// DurableObjectMigration is a Durable Object class migration declaration.
type DurableObjectMigration struct {
	Tag              string   `json:"tag"`
	NewSQLiteClasses []string `json:"new_sqlite_classes,omitempty"`
	NewClasses       []string `json:"new_classes,omitempty"`
}

// UploadWorkerParams holds all parameters for uploading a Worker module script.
type UploadWorkerParams struct {
	ScriptName    string
	Bundle        []byte
	Bindings      []WorkerBinding
	DOMiddleMigrations []DurableObjectMigration
	CompatDate    string
}

// UploadWorkerScript uploads (creates or updates) a Worker module script with bindings.
func (c *Client) UploadWorkerScript(ctx context.Context, params UploadWorkerParams) (*WorkerScript, error) {
	// Cloudflare module Worker upload uses multipart/form-data.
	// Part 1: "metadata" JSON with bindings.
	// Part 2: "index.js" with the Worker module bundle.

	type workerMetadata struct {
		MainModule        string                   `json:"main_module"`
		CompatibilityDate string                   `json:"compatibility_date,omitempty"`
		Bindings          []WorkerBinding          `json:"bindings"`
		Migrations        *struct {
			OldTag string                    `json:"old_tag,omitempty"`
			NewTag string                    `json:"new_tag,omitempty"`
			Steps  []DurableObjectMigration  `json:"steps"`
		} `json:"migrations,omitempty"`
	}

	compatDate := params.CompatDate
	if compatDate == "" {
		compatDate = "2024-01-01"
	}

	meta := workerMetadata{
		MainModule:        "index.js",
		CompatibilityDate: compatDate,
		Bindings:          params.Bindings,
	}
	if len(params.DOMiddleMigrations) > 0 {
		// Use the last step's Tag as new_tag (Cloudflare tracks migration state by tag).
		newTag := params.DOMiddleMigrations[len(params.DOMiddleMigrations)-1].Tag
		meta.Migrations = &struct {
			OldTag string                   `json:"old_tag,omitempty"`
			NewTag string                   `json:"new_tag,omitempty"`
			Steps  []DurableObjectMigration `json:"steps"`
		}{
			NewTag: newTag,
			Steps:  params.DOMiddleMigrations,
		}
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal worker metadata: %w", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Write metadata part.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="metadata"`)
	h.Set("Content-Type", "application/json")
	metaPart, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, err
	}

	// Write the Worker bundle part.
	bh := make(textproto.MIMEHeader)
	bh.Set("Content-Disposition", `form-data; name="index.js"; filename="index.js"`)
	bh.Set("Content-Type", "application/javascript+module")
	bundlePart, err := mw.CreatePart(bh)
	if err != nil {
		return nil, err
	}
	if _, err := bundlePart.Write(params.Bundle); err != nil {
		return nil, err
	}
	mw.Close()

	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s", c.accountID, params.ScriptName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.uploadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload worker script: %w", err)
	}
	defer resp.Body.Close()

	var env apiEnvelope
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse upload response: %w", err)
	}
	if !env.Success {
		return nil, envelopeError(env.Errors)
	}
	var script WorkerScript
	if err := json.Unmarshal(env.Result, &script); err != nil {
		return nil, fmt.Errorf("decode worker script response: %w", err)
	}
	return &script, nil
}

// GetWorkerScript returns the Worker script metadata, or nil if it does not exist.
func (c *Client) GetWorkerScript(ctx context.Context, scriptName string) (*WorkerScript, error) {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s", c.accountID, scriptName)
	var script WorkerScript
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &script); err != nil {
		cfErr := asCFError(err)
		if cfErr != nil && (cfErr.Code == 10007 || cfErr.Code == 10090) {
			return nil, nil
		}
		return nil, fmt.Errorf("get worker script %q: %w", scriptName, err)
	}
	return &script, nil
}

// DeleteWorkerScript deletes a Worker script by name.
func (c *Client) DeleteWorkerScript(ctx context.Context, scriptName string) error {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s", c.accountID, scriptName)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete worker script %q: %w", scriptName, err)
	}
	return nil
}

// ── Worker vars ───────────────────────────────────────────────────────────────

// SetWorkerVars updates or creates non-secret Worker environment variables.
// These are sent as part of the script metadata, so the script must already exist.
func (c *Client) SetWorkerVars(ctx context.Context, scriptName string, vars map[string]string) error {
	if len(vars) == 0 {
		return nil
	}
	type varBinding struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type settingsBody struct {
		Bindings []varBinding `json:"bindings"`
	}
	bindings := make([]varBinding, 0, len(vars))
	for k, v := range vars {
		bindings = append(bindings, varBinding{Name: k, Type: "plain_text", Text: v})
	}
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/settings", c.accountID, scriptName)
	if err := c.doJSONPatch(ctx, path, settingsBody{Bindings: bindings}, nil); err != nil {
		return fmt.Errorf("set worker vars for %q: %w", scriptName, err)
	}
	return nil
}

// ── Worker secrets ───────────────────────────────────────────────────────────

// SetWorkerSecret sets a single Worker secret. The value is never logged.
func (c *Client) SetWorkerSecret(ctx context.Context, scriptName, secretName, secretValue string) error {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets", c.accountID, scriptName)
	body := map[string]string{
		"name":  secretName,
		"text":  secretValue,
		"type":  "secret_text",
	}
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("set worker secret %q: %w (value redacted)", secretName, err)
	}
	return nil
}

// ListWorkerSecretNames returns the names of secrets currently set on a Worker script.
func (c *Client) ListWorkerSecretNames(ctx context.Context, scriptName string) ([]string, error) {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets", c.accountID, scriptName)
	type secretMeta struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var secrets []secretMeta
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &secrets); err != nil {
		return nil, fmt.Errorf("list worker secrets for %q: %w", scriptName, err)
	}
	names := make([]string, 0, len(secrets))
	for _, s := range secrets {
		names = append(names, s.Name)
	}
	return names, nil
}

// ── Workers.dev subdomain ─────────────────────────────────────────────────────

// GetWorkerSubdomain returns the workers.dev subdomain for the account, if enabled.
// Returns empty string if the subdomain is not enabled.
func (c *Client) GetWorkerSubdomain(ctx context.Context) (string, error) {
	type subdomainResult struct {
		Subdomain string `json:"subdomain"`
	}
	var result subdomainResult
	path := fmt.Sprintf("/accounts/%s/workers/subdomain", c.accountID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return "", nil // best-effort, non-fatal
	}
	return result.Subdomain, nil
}

// EnableWorkerSubdomainRoute enables the workers.dev route for a Worker script.
func (c *Client) EnableWorkerSubdomainRoute(ctx context.Context, scriptName string) error {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/subdomain", c.accountID, scriptName)
	body := map[string]bool{"enabled": true}
	if err := c.doJSON(ctx, http.MethodPost, path, body, nil); err != nil {
		return fmt.Errorf("enable subdomain route for %q: %w", scriptName, err)
	}
	return nil
}

// ── Queues ────────────────────────────────────────────────────────────────────

// Queue represents a Cloudflare Queue.
type Queue struct {
	QueueID   string `json:"queue_id"`
	QueueName string `json:"queue_name"`
}

// QueueConsumerSettings holds the configurable consumer behavior.
type QueueConsumerSettings struct {
	BatchSize     int `json:"batch_size,omitempty"`
	MaxRetries    int `json:"max_retries,omitempty"`
	MaxWaitTimeMs int `json:"max_wait_time_ms,omitempty"`
}

// QueueConsumer represents a consumer of a Cloudflare Queue.
type QueueConsumer struct {
	ConsumerID      string                `json:"consumer_id"`
	ScriptName      string                `json:"script_name"`
	DeadLetterQueue string                `json:"dead_letter_queue"`
	Type            string                `json:"type"`
	Settings        QueueConsumerSettings `json:"settings"`
}

// WorkerSchedule represents a single Worker cron trigger.
type WorkerSchedule struct {
	Cron string `json:"cron"`
}

// ListQueues returns all queues for the account.
func (c *Client) ListQueues(ctx context.Context) ([]Queue, error) {
	var queues []Queue
	if err := c.doJSON(ctx, http.MethodGet, c.accountPath("/queues"), nil, &queues); err != nil {
		return nil, fmt.Errorf("list queues: %w", err)
	}
	return queues, nil
}

// FindQueueByName returns the queue with the given name, or nil if not found.
func (c *Client) FindQueueByName(ctx context.Context, name string) (*Queue, error) {
	queues, err := c.ListQueues(ctx)
	if err != nil {
		return nil, err
	}
	for i := range queues {
		if queues[i].QueueName == name {
			return &queues[i], nil
		}
	}
	return nil, nil
}

// CreateQueue creates a new queue idempotently. If a queue with the given name already exists, it is returned.
func (c *Client) CreateQueue(ctx context.Context, name string) (*Queue, error) {
	existing, err := c.FindQueueByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	body := map[string]string{"queue_name": name}
	var q Queue
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/queues"), body, &q); err != nil {
		return nil, fmt.Errorf("create queue %q: %w", name, err)
	}
	return &q, nil
}

// DeleteQueueByID deletes a queue by its ID. Missing resources are not treated as an error.
func (c *Client) DeleteQueueByID(ctx context.Context, queueID string) error {
	err := c.doJSON(ctx, http.MethodDelete, c.accountPath("/queues/"+queueID), nil, nil)
	if err == nil {
		return nil
	}
	if cfErr := asCFError(err); cfErr != nil && (cfErr.Code == 404 || cfErr.Code == 10010 || cfErr.Code == 11001) {
		return nil
	}
	return fmt.Errorf("delete queue %s: %w", queueID, err)
}

// DeleteQueueByName deletes a queue by name. Missing resources are not treated as an error.
func (c *Client) DeleteQueueByName(ctx context.Context, name string) error {
	q, err := c.FindQueueByName(ctx, name)
	if err != nil {
		return err
	}
	if q == nil {
		return nil
	}
	return c.DeleteQueueByID(ctx, q.QueueID)
}

// ListQueueConsumers returns all consumers attached to a queue.
func (c *Client) ListQueueConsumers(ctx context.Context, queueID string) ([]QueueConsumer, error) {
	var consumers []QueueConsumer
	path := c.accountPath("/queues/" + queueID + "/consumers")
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &consumers); err != nil {
		return nil, fmt.Errorf("list queue consumers for %s: %w", queueID, err)
	}
	return consumers, nil
}

// CreateOrUpdateQueueConsumer idempotently attaches a Worker consumer to a queue.
// If a consumer for the same script already exists with matching settings, this is a no-op.
// If it exists with different settings, it is deleted and recreated.
func (c *Client) CreateOrUpdateQueueConsumer(ctx context.Context, queueID, scriptName, dlqName string, settings QueueConsumerSettings) error {
	consumers, err := c.ListQueueConsumers(ctx, queueID)
	if err != nil {
		return err
	}
	for _, cons := range consumers {
		if cons.ScriptName == scriptName && cons.Type == "worker" {
			if cons.Settings.BatchSize == settings.BatchSize &&
				cons.Settings.MaxRetries == settings.MaxRetries &&
				cons.Settings.MaxWaitTimeMs == settings.MaxWaitTimeMs &&
				cons.DeadLetterQueue == dlqName {
				return nil // already configured correctly
			}
			if delErr := c.DeleteQueueConsumer(ctx, queueID, cons.ConsumerID); delErr != nil {
				return delErr
			}
			break
		}
	}
	type consumerBody struct {
		Type            string                `json:"type"`
		ScriptName      string                `json:"script_name"`
		DeadLetterQueue string                `json:"dead_letter_queue,omitempty"`
		Settings        QueueConsumerSettings `json:"settings,omitempty"`
	}
	body := consumerBody{
		Type:            "worker",
		ScriptName:      scriptName,
		DeadLetterQueue: dlqName,
		Settings:        settings,
	}
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/queues/"+queueID+"/consumers"), body, nil); err != nil {
		return fmt.Errorf("create queue consumer for queue %s: %w", queueID, err)
	}
	return nil
}

// DeleteQueueConsumer removes a consumer from a queue. Missing resources are not treated as an error.
func (c *Client) DeleteQueueConsumer(ctx context.Context, queueID, consumerID string) error {
	path := c.accountPath("/queues/" + queueID + "/consumers/" + consumerID)
	err := c.doJSON(ctx, http.MethodDelete, path, nil, nil)
	if err == nil {
		return nil
	}
	if cfErr := asCFError(err); cfErr != nil && cfErr.Code == 404 {
		return nil
	}
	return fmt.Errorf("delete queue consumer %s: %w", consumerID, err)
}

// ListWorkerSchedules returns the cron schedules for a Worker script.
func (c *Client) ListWorkerSchedules(ctx context.Context, scriptName string) ([]WorkerSchedule, error) {
	type result struct {
		Schedules []WorkerSchedule `json:"schedules"`
	}
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/schedules", c.accountID, scriptName)
	var r result
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &r); err != nil {
		return nil, fmt.Errorf("list worker schedules for %q: %w", scriptName, err)
	}
	return r.Schedules, nil
}

// UpdateWorkerSchedules replaces all cron schedules for a Worker. Pass an empty slice to clear all schedules.
func (c *Client) UpdateWorkerSchedules(ctx context.Context, scriptName string, crons []string) error {
	type scheduleEntry struct {
		Cron string `json:"cron"`
	}
	body := make([]scheduleEntry, 0, len(crons))
	for _, cron := range crons {
		body = append(body, scheduleEntry{Cron: cron})
	}
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/schedules", c.accountID, scriptName)
	if err := c.doJSON(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("update worker schedules for %q: %w", scriptName, err)
	}
	return nil
}

// DeleteWorkerSchedules removes all cron schedules for a Worker.
func (c *Client) DeleteWorkerSchedules(ctx context.Context, scriptName string) error {
	return c.UpdateWorkerSchedules(ctx, scriptName, nil)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (c *Client) accountPath(suffix string) string {
	return fmt.Sprintf("/accounts/%s%s", c.accountID, suffix)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := path
	if !strings.HasPrefix(path, "http") {
		url = c.baseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Cloudflare always wraps in an envelope on non-streaming responses.
	var env apiEnvelope
	if jsonErr := json.Unmarshal(data, &env); jsonErr == nil {
		if !env.Success {
			return envelopeError(env.Errors)
		}
		if out != nil && len(env.Result) > 0 && string(env.Result) != "null" {
			if err := json.Unmarshal(env.Result, out); err != nil {
				return fmt.Errorf("decode result: %w", err)
			}
		}
		return nil
	}

	// Fall back to raw status check for non-JSON or bare responses (e.g. DELETE 204).
	if resp.StatusCode >= 400 {
		return &APIError{Code: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doJSONPatch sends a PATCH request with JSON body.
func (c *Client) doJSONPatch(ctx context.Context, path string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal PATCH request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	url := path
	if !strings.HasPrefix(path, "http") {
		url = c.baseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, reqBody)
	if err != nil {
		return fmt.Errorf("build PATCH request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var env apiEnvelope
	if jsonErr := json.Unmarshal(data, &env); jsonErr == nil {
		if !env.Success {
			return envelopeError(env.Errors)
		}
		if out != nil && len(env.Result) > 0 {
			_ = json.Unmarshal(env.Result, out)
		}
		return nil
	}
	if resp.StatusCode >= 400 {
		return &APIError{Code: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	return nil
}

func envelopeError(errs []APIError) error {
	if len(errs) == 0 {
		return &APIError{Code: 0, Message: "unknown cloudflare error"}
	}
	return &errs[0]
}

func asCFError(err error) *APIError {
	if err == nil {
		return nil
	}
	cfErr, ok := err.(*APIError)
	if ok {
		return cfErr
	}
	return nil
}
