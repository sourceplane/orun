package actions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
)

// orun.integrations/reconcile@v1 — bring a workspace's brokered secrets to a
// declared desired state (orun-bootstrap-engine BE-O5b).
//
// # Why this is a reconcile and not a create
//
// A baseline declares which secrets a bootstrap needs — key, provider,
// template — and a script walks that table calling create for each, skipping
// what exists and re-making what was orphaned. It is a reconcile written out
// longhand, and this is the verb it was reaching for.
//
// # Brokered, which is the whole point
//
// A brokered secret carries NO VALUE. It is a pointer at a connection and a
// scope template, and the value is minted just-in-time at resolve. So this
// action can create every credential a bootstrap needs while never holding,
// logging, or being able to read one — and `Params` is deliberately the only
// free-form field, validated server-side against the template's declared names.
//
// # Keys that exist are kept
//
// Re-running a phase must not rotate a credential the rest of the product is
// already using. An existing key is left exactly as it is.
func init() {
	register(Spec{
		ID:      "orun.integrations/reconcile@v1",
		Summary: "Create the declared brokered secrets that do not exist yet, minted from the workspace's connections",
		Doing:   "Minting this phase's secrets from your connections.",
		Params: append(orgParams(),
			Param{Name: "provider", Type: ParamString, Required: true,
				Description: "the connected provider the secrets are minted from"},
			Param{Name: "keys", Type: ParamStringList, Required: true,
				Description: "secret keys to ensure"},
			Param{Name: "template", Type: ParamString, Required: true,
				Description: "the broker scope template each key is minted under"},
			Param{Name: "project", Type: ParamString,
				Description: "project scope; empty uses the workspace scope"},
			Param{Name: "connection", Type: ParamString,
				Description: "which connection to mint from when the provider has several: its id, display name or account login; empty uses the first active one"},
		),
		Outputs: []string{"created", "kept", "connection"},
	}, runIntegrationsReconcile)
}

func runIntegrationsReconcile(ctx context.Context, in Input) (Result, error) {
	client, org, err := configClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	return reconcileOn(ctx, client, org, in)
}

func reconcileOn(ctx context.Context, c secretsPlane, org string, in Input) (Result, error) {
	provider := strings.ToLower(strings.TrimSpace(StringParam(in, "provider")))
	template := StringParam(in, "template")
	keys := StringListParam(in, "keys")
	scope := secretScope(org, in)

	conns, err := c.ListConnections(ctx, org)
	if err != nil {
		return Result{}, fmt.Errorf("reading connections for %s: %w", org, err)
	}
	var connectionID string
	if ref := strings.TrimSpace(StringParam(in, "connection")); ref != "" {
		// MCX-D7: a pinned connection. A name two connections share is
		// refused rather than guessed; a name nothing matches yet is a wait,
		// like a provider not connected yet.
		matched := matchConnectionRef(conns, provider, ref)
		if len(matched) > 1 {
			return Result{}, fmt.Errorf("connection %q matches %d %s connections in workspace %s; pin one by id: %s",
				ref, len(matched), provider, org, connectionCandidates(conns, provider))
		}
		if len(matched) == 0 {
			return Result{Outputs: map[string]string{"created": "", "kept": "", "connection": ""},
				Pending: &Pending{Reason: fmt.Sprintf("no active %s connection %q in workspace %s (active: %s)",
					provider, ref, org, connectionCandidates(conns, provider))}}, nil
		}
		connectionID = matched[0].ID
	} else {
		// Unpinned: the first active connection of the provider, exactly as
		// before references existed.
		for _, conn := range conns {
			if strings.EqualFold(conn.Provider, provider) && strings.EqualFold(conn.Status, "active") {
				connectionID = conn.ID
				break
			}
		}
	}
	if connectionID == "" {
		// Not connected yet is a wait, not a failure — the same reasoning as
		// doctor/check. A consent a person has not clicked is not a broken build.
		return Result{Outputs: map[string]string{"created": "", "kept": "", "connection": ""},
			Pending: &Pending{Reason: fmt.Sprintf("%s is not connected in workspace %s", provider, org)}}, nil
	}

	existing, _, err := c.ListSecrets(ctx, scope, true)
	if err != nil {
		return Result{}, fmt.Errorf("reading secrets for %s: %w", org, err)
	}
	have := map[string]bool{}
	for _, s := range existing {
		have[s.SecretKey] = true
	}

	var created, kept []string
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if have[key] {
			kept = append(kept, key)
			continue
		}
		if _, err := c.CreateSecret(ctx, scope, configsurface.CreateSecretRequest{
			SecretKey: key,
			Binding:   &configsurface.SecretBrokerBinding{ConnectionID: connectionID, Template: template},
		}); err != nil {
			// Resource-hiding masks authorization as not-found: when the READS
			// above worked and the WRITE does not, the credential is almost
			// certainly below the admin floor. Say so, because the raw error
			// sends people hunting for a missing scope.
			return Result{Outputs: map[string]string{
					"created": strings.Join(created, ","), "kept": strings.Join(kept, ","), "connection": connectionID,
				}}, fmt.Errorf(
					"creating %s failed. The reads above succeeded, so if this is a not-found, this credential's role is below ADMIN — brokered secret creation requires an admin-role key. Underlying error: %w", key, err)
		}
		created = append(created, key)
	}
	sort.Strings(created)
	sort.Strings(kept)
	return Result{Outputs: map[string]string{
		"created": strings.Join(created, ","), "kept": strings.Join(kept, ","), "connection": connectionID,
	}}, nil
}
