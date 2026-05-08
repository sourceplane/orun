// Package backendbundle provides access to the embedded Orun backend artifacts:
// the Cloudflare Worker bundle, D1 SQL migrations, and a manifest describing
// default resource names and binding names.
//
// To refresh artifacts from sourceplane/orun-backend:
//
//	cp orun-backend/apps/worker/dist/index.js orun/internal/backendbundle/embed/worker/index.js
//	cp orun-backend/migrations/*.sql orun/internal/backendbundle/embed/migrations/
//	# Update embed/manifest.json with the new backend commit SHA and date.
package backendbundle

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"embed"
)

//go:embed embed/worker/index.js
var workerBundle []byte

//go:embed embed/manifest.json
var manifestJSON []byte

//go:embed embed/migrations
var migrationFS embed.FS

// Migration is a single embedded SQL migration.
type Migration struct {
	Name string
	SQL  string
}

// ManifestConsumerSettings holds the default queue consumer configuration for the embedded backend.
type ManifestConsumerSettings struct {
	BatchSize     int `json:"batchSize"`
	MaxRetries    int `json:"maxRetries"`
	MaxWaitTimeMs int `json:"maxWaitTimeMs"`
}

// Manifest describes the embedded backend artifact metadata.
type Manifest struct {
	BackendCommitSHA         string                   `json:"backendCommitSHA"`
	BundleDate               string                   `json:"bundleDate"`
	WorkerScriptName         string                   `json:"workerScriptName"`
	D1DatabaseName           string                   `json:"d1DatabaseName"`
	R2BucketName             string                   `json:"r2BucketName"`
	CatalogQueueName         string                   `json:"catalogQueueName"`
	CatalogDLQName           string                   `json:"catalogDLQName"`
	CatalogCron              string                   `json:"catalogCron"`
	CatalogConsumerSettings  ManifestConsumerSettings `json:"catalogConsumerSettings"`
	DurableObjectClasses     []string                 `json:"durableObjectClasses"`
	Bindings                 struct {
		DurableObjects []string `json:"durableObjects"`
		D1             string   `json:"d1"`
		R2             string   `json:"r2"`
		Queue          string   `json:"queue"`
	} `json:"bindings"`
	Vars    []string `json:"vars"`
	Secrets []string `json:"secrets"`
}

// WorkerBundle returns the raw bytes of the embedded Worker module bundle.
func WorkerBundle() []byte {
	return workerBundle
}

// GetManifest returns the decoded bundle manifest.
func GetManifest() (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// Migrations returns all embedded D1 migrations in lexical filename order.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "embed/migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, name := range names {
		data, err := migrationFS.ReadFile("embed/migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		out = append(out, Migration{
			Name: name,
			SQL:  strings.TrimSpace(string(data)),
		})
	}
	return out, nil
}
