package materialize

import (
	"context"
	"fmt"
	"strings"
)

// CloudflareWorkerTarget is the target id for the cloudflare-worker adapter.
const CloudflareWorkerTarget = "cloudflare-worker"

// WorkerSecretSetter is the minimal Cloudflare surface the adapter depends on:
// exactly *internal/cloudflare.Client.SetWorkerSecret. Depending on this
// interface (not the concrete client) keeps the adapter testable without real
// Cloudflare credentials. The value is never logged by either side.
type WorkerSecretSetter interface {
	SetWorkerSecret(ctx context.Context, scriptName, secretName, secretValue string) error
}

// cloudflareWorkerAdapter delivers secrets into a Cloudflare Worker's secret
// bindings via SetWorkerSecret (internal/cloudflare/client.go). Re-running is
// idempotent — SetWorkerSecret overwrites by name.
type cloudflareWorkerAdapter struct {
	client WorkerSecretSetter
}

// NewCloudflareWorkerAdapter wraps a Cloudflare client (or any
// WorkerSecretSetter) as the cloudflare-worker materialize adapter.
func NewCloudflareWorkerAdapter(client WorkerSecretSetter) Adapter {
	return &cloudflareWorkerAdapter{client: client}
}

func (a *cloudflareWorkerAdapter) Name() string { return CloudflareWorkerTarget }

func (a *cloudflareWorkerAdapter) Put(ctx context.Context, target TargetBinding, key, value string) error {
	if a.client == nil {
		return fmt.Errorf("cloudflare-worker adapter has no Cloudflare client configured")
	}
	script := strings.TrimSpace(target.ScriptName)
	if script == "" {
		return fmt.Errorf("cloudflare-worker adapter requires a Worker script name (none derived for this target)")
	}
	// The value is passed straight through and never logged; SetWorkerSecret
	// redacts it from its own error path too.
	return a.client.SetWorkerSecret(ctx, script, key, value)
}
