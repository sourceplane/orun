package main

import (
	"context"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/materialize"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/runner"
)

// attachMaterialize wires the deploy-time materialization surface onto the
// runner (orun-secrets SEC6, runner-integration.md §6): the typed adapter
// registry that delivers resolved secrets into deployed applications, and the
// best-effort sync-provenance recorder. The runner only calls Put + record, so
// it stays decoupled from any cloud specifics.
//
// Both are nil-safe and best-effort to wire: a run whose jobs declare no
// materialize block is unaffected, and a deploy environment without Cloudflare
// credentials simply leaves the registry unset (a materialize job then fails
// loudly at run time, as design §8.3 requires).
func attachMaterialize(r *runner.Runner, backendURL string, tokenSrc remotestate.TokenSource, org, project string) {
	// The Cloudflare client is built from the credentials the deploy job already
	// has (CLOUDFLARE_API_TOKEN / CLOUDFLARE_ACCOUNT_ID). Missing creds → no
	// adapter wired.
	cfClient, _, _, err := newCFClient("orun-cli/" + version)
	if err != nil {
		return
	}
	reg := materialize.NewRegistry()
	reg.Register(materialize.NewCloudflareWorkerAdapter(cfClient))
	r.MaterializeAdapters = reg

	// The sync recorder needs a project-scoped config surface. Without a resolved
	// org/project the value is still delivered; only provenance is skipped.
	if strings.TrimSpace(org) == "" || strings.TrimSpace(project) == "" {
		return
	}
	cs := configsurface.NewClient(backendURL, version, tokenSrc)
	scope := configsurface.Scope{Kind: configsurface.ScopeProject, Org: org, Project: project}
	r.MaterializeSyncRecorder = func(ctx context.Context, rec materialize.SyncRecord) error {
		return cs.RecordSync(ctx, scope, configsurface.RecordSyncRequest{
			SecretKey: rec.SecretKey,
			Version:   rec.Version,
			Target:    rec.Target,
			EntityRef: rec.EntityRef,
			RunID:     rec.RunID,
		})
	}
}
