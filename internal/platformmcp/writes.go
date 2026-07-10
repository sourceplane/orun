package platformmcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The 6 write tools (orun-mcp UM2, design §3). Write rails: every write
// carries an Idempotency-Key — a caller-supplied one passed through verbatim
// (validated to 1-255 printable ASCII, the manifest contract) or a fresh
// `mcp_<uuid>` minted per logical attempt; retries at the transport replay
// under the same key. Wire routes are the PlatformAPI seam's business
// (pinned in remotestate/platform.go); this layer owns argument validation,
// the flag upsert and webhook fan-out flows, and the invite-token guard.

// resolveIdemKey returns the write's Idempotency-Key: the supplied
// `idempotencyKey` verbatim when present, else a fresh auto key.
func resolveIdemKey(a argmap) (string, error) {
	v, supplied := a["idempotencyKey"]
	if !supplied {
		return newIdemKey(), nil
	}
	s, _ := v.(string)
	if !validIdemKey(s) {
		return "", &remotestate.APIError{Code: "validation_failed",
			Message: "idempotencyKey must be 1-255 printable ASCII characters"}
	}
	return s, nil
}

// validIdemKey enforces the manifest contract: 1-255 printable ASCII.
func validIdemKey(s string) bool {
	if len(s) == 0 || len(s) > 255 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// newIdemKey mints the per-attempt auto key: "mcp_" + a v4 UUID from
// crypto/rand (40 chars, printable ASCII).
func newIdemKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("platformmcp: crypto/rand: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("mcp_%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (p *Provider) callWrite(ctx context.Context, name string, a argmap) (string, error) {
	key, err := resolveIdemKey(a)
	if err != nil {
		return "", err
	}
	ws := a.str("workspace")
	switch name {
	case "project_create":
		if err := requireStr(a, name, "name"); err != nil {
			return "", err
		}
		body := map[string]interface{}{"name": a.str("name")}
		if slug := a.str("slug"); slug != "" {
			body["slug"] = slug
		}
		page, err := p.API.CreateProject(ctx, ws, body, key)
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("created project %s in %s", a.str("name"), ws), page)

	case "environment_create":
		if err := requireStr(a, name, "project", "name"); err != nil {
			return "", err
		}
		body := map[string]interface{}{"name": a.str("name")}
		if slug := a.str("slug"); slug != "" {
			body["slug"] = slug
		}
		page, err := p.API.CreateProjectEnvironment(ctx, ws, a.str("project"), body, key)
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("created environment %s under project %s", a.str("name"), a.str("project")), page)

	case "flag_set":
		return p.flagSet(ctx, a, ws, key)

	case "webhook_create":
		return p.webhookCreate(ctx, a, ws, key)

	case "webhook_delivery_replay":
		if err := requireStr(a, name, "delivery"); err != nil {
			return "", err
		}
		page, err := p.API.ReplayWebhookDelivery(ctx, ws, a.str("delivery"), key)
		if err != nil {
			return "", err
		}
		return emit("replayed webhook delivery "+a.str("delivery")+" — this is the new attempt", page)

	case "member_invite":
		if err := requireStr(a, name, "email", "role"); err != nil {
			return "", err
		}
		page, err := p.API.CreateInvitation(ctx, ws,
			map[string]interface{}{"email": a.str("email"), "role": a.str("role")}, key)
		if err != nil {
			return "", err
		}
		// The TS-plane guard: the one-time accept token never reaches tool
		// output — the invitee accepts from their own signed-in session.
		page.Data = stripInviteToken(page.Data)
		return emit(fmt.Sprintf("invited %s to %s as %s (accept token withheld by design)",
			a.str("email"), ws, a.str("role")), page)

	default:
		return "", fmt.Errorf("unknown tool %s", name)
	}
}

// flagSet is the upsert flow (mirroring the TS tool): list the flags at the
// exact scope, PATCH the one whose key matches flagKey, else POST a new one.
// The single write carries the attempt's key; the list is a read.
func (p *Provider) flagSet(ctx context.Context, a argmap, ws, key string) (string, error) {
	if err := requireStr(a, "flag_set", "flagKey"); err != nil {
		return "", err
	}
	_, hasEnabled := a["enabled"]
	_, hasValue := a["value"]
	if !hasEnabled && !hasValue {
		return "", &remotestate.APIError{Code: "validation_failed",
			Message: "flag_set requires at least one of enabled or value"}
	}
	scope, scopeName, err := configScope(a, "flag_set", ws)
	if err != nil {
		return "", err
	}
	flagKey := a.str("flagKey")
	flags, err := p.API.ListFeatureFlags(ctx, scope)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{}
	if hasEnabled {
		body["enabled"] = a["enabled"]
	}
	if hasValue {
		body["value"] = a["value"]
	}
	if id, ok := findFlagID(flags.Data, flagKey); ok {
		page, err := p.API.UpdateFeatureFlag(ctx, scope, id, body, key)
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("updated flag %s at %s scope", flagKey, scopeName), page)
	}
	// CreateFeatureFlag body carries the key as flagKey (contracts config.ts).
	body["flagKey"] = flagKey
	page, err := p.API.CreateFeatureFlag(ctx, scope, body, key)
	if err != nil {
		return "", err
	}
	return emit(fmt.Sprintf("created flag %s at %s scope", flagKey, scopeName), page)
}

// webhookCreate creates the endpoint, then fans each `events` entry out to a
// subscription create under a key derived deterministically from the base
// (`<base>:sub<i>`) — a retry with the same base key replays every leg.
func (p *Provider) webhookCreate(ctx context.Context, a argmap, ws, key string) (string, error) {
	if err := requireStr(a, "webhook_create", "url"); err != nil {
		return "", err
	}
	body := map[string]interface{}{"url": a.str("url")}
	if v := a.str("name"); v != "" {
		body["name"] = v
	}
	if v := a.str("description"); v != "" {
		body["description"] = v
	}
	endpoint, err := p.API.CreateWebhookEndpoint(ctx, ws, a.str("project"), body, key)
	if err != nil {
		return "", err
	}
	events := a.strList("events")
	if len(events) == 0 {
		return emit("created webhook endpoint in "+ws+" (no subscriptions — it will receive nothing until subscribed)", endpoint)
	}
	endpointID := jsonStrField(endpoint.Data, "id")
	subs := make([]json.RawMessage, 0, len(events))
	for i, ev := range events {
		sub, err := p.API.CreateWebhookSubscription(ctx, ws,
			map[string]interface{}{"endpointId": endpointID, "eventType": ev},
			fmt.Sprintf("%s:sub%d", key, i))
		if err != nil {
			return "", err
		}
		subs = append(subs, sub.Data)
	}
	return emit(fmt.Sprintf("created webhook endpoint in %s with %d subscriptions", ws, len(subs)),
		map[string]interface{}{"endpoint": endpoint.Data, "subscriptions": subs})
}

func (a argmap) strList(k string) []string {
	items, _ := a[k].([]interface{})
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// findFlagID scans a feature-flag list page for the row whose key equals
// flagKey, returning its id. The rows may be the data itself or sit under a
// "flags"/"items" key (findEntity's tolerance).
func findFlagID(data json.RawMessage, flagKey string) (string, bool) {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) != nil {
			return "", false
		}
		for _, k := range []string{"flags", "items"} {
			if raw, ok := obj[k]; ok && json.Unmarshal(raw, &items) == nil {
				break
			}
		}
	}
	for _, item := range items {
		var row struct {
			ID      string `json:"id"`
			Key     string `json:"key"`
			FlagKey string `json:"flagKey"`
		}
		if json.Unmarshal(item, &row) == nil && (row.Key == flagKey || row.FlagKey == flagKey) && row.ID != "" {
			return row.ID, true
		}
	}
	return "", false
}

// jsonStrField extracts a top-level string field from a JSON object ("" when
// absent).
func jsonStrField(data json.RawMessage, field string) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return ""
	}
	var s string
	if raw, ok := obj[field]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// stripInviteToken removes the one-time accept token from an invitation
// payload: every "token" key at any depth (the platform returns it in a
// delivery.token-shaped field; over-stripping is the safe side of the guard).
func stripInviteToken(data json.RawMessage) json.RawMessage {
	var v interface{}
	if json.Unmarshal(data, &v) != nil {
		return data
	}
	dropTokens(v)
	out, err := json.Marshal(v)
	if err != nil {
		return data
	}
	return out
}

func dropTokens(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		delete(t, "token")
		for _, child := range t {
			dropTokens(child)
		}
	case []interface{}:
		for _, child := range t {
			dropTokens(child)
		}
	}
}
