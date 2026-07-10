package platformmcp

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The entitlement gate (orun-mcp UM2, design §3): the first workspace-carrying
// tool call (reads and writes alike) lazily checks the workspace's
// feature.mcp_server entitlement via the public billing-entitlements read.
// Posture mirrors the TS transports: a missing row or a failed read GRANTS
// (fail-open — billing plumbing must never brick the tool plane); only an
// explicit enabled:false denies, as the platform's entitlement_required
// error. Verdicts cache for 60s per workspace, small-capped.

const (
	mcpEntitlementFeature = "feature.mcp_server"
	entitlementTTL        = 60 * time.Second
	entitlementCacheMax   = 64
)

type entitlementVerdict struct {
	granted bool
	expires time.Time
}

type entitlementGate struct {
	mu    sync.Mutex
	cache map[string]entitlementVerdict
	now   func() time.Time // test seam; time.Now when nil
}

// check returns nil when workspace ws may use the MCP plane, or the
// entitlement_required error on an explicit denial.
func (g *entitlementGate) check(ctx context.Context, api PlatformAPI, ws string) error {
	g.mu.Lock()
	if g.now == nil {
		g.now = time.Now
	}
	if v, ok := g.cache[ws]; ok && g.now().Before(v.expires) {
		g.mu.Unlock()
		return verdictErr(v, ws)
	}
	g.mu.Unlock()

	granted := true // fail-open default
	if page, err := api.ListEntitlements(ctx, ws); err == nil && page != nil {
		granted = entitlementEnabled(page.Data)
	}
	v := entitlementVerdict{granted: granted}
	g.mu.Lock()
	if g.cache == nil {
		g.cache = make(map[string]entitlementVerdict, 1)
	} else if len(g.cache) >= entitlementCacheMax {
		g.cache = make(map[string]entitlementVerdict, 1)
	}
	v.expires = g.now().Add(entitlementTTL)
	g.cache[ws] = v
	g.mu.Unlock()
	return verdictErr(v, ws)
}

// seed pre-fills a verdict (tests, and reused by check).
func (g *entitlementGate) seed(ws string, granted bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.now == nil {
		g.now = time.Now
	}
	if g.cache == nil {
		g.cache = make(map[string]entitlementVerdict, 1)
	}
	g.cache[ws] = entitlementVerdict{granted: granted, expires: g.now().Add(entitlementTTL)}
}

func verdictErr(v entitlementVerdict, ws string) error {
	if v.granted {
		return nil
	}
	return &remotestate.APIError{Code: "entitlement_required",
		Message: "the MCP server feature (" + mcpEntitlementFeature + ") is not enabled for workspace " + ws +
			" — ask a billing admin to enable it or upgrade the plan"}
}

// entitlementEnabled scans an entitlements page for the feature.mcp_server
// row. Missing row (or unparseable payload) ⇒ true; only an explicit
// enabled:false ⇒ false. Rows may be the data itself or sit under an
// "entitlements"/"items" key; the feature id may ride as feature or key.
func entitlementEnabled(data json.RawMessage) bool {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) != nil {
			return true
		}
		for _, k := range []string{"entitlements", "items"} {
			if raw, ok := obj[k]; ok && json.Unmarshal(raw, &items) == nil {
				break
			}
		}
	}
	for _, item := range items {
		var row struct {
			Feature string `json:"feature"`
			Key     string `json:"key"`
			Enabled *bool  `json:"enabled"`
		}
		if json.Unmarshal(item, &row) != nil {
			continue
		}
		if row.Feature == mcpEntitlementFeature || row.Key == mcpEntitlementFeature {
			return row.Enabled == nil || *row.Enabled
		}
	}
	return true
}
