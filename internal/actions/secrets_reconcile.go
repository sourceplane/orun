package actions

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

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
//
// # Several templates, one hook
//
// A product usually needs secrets minted under more than one template (a
// deploy token, a database token, the account id), and `keys` + `template`
// take one template per hook — so a baseline wrote one hook per template, and
// each one read the connections and the secrets again before its create, one
// after another. `secrets: [{key, template}, …]` declares them together: the
// connections and the existing secrets are read once, and the missing keys are
// created side by side. `keys` + `template` stay as they were, and their keys
// are created side by side too.
func init() {
	register(Spec{
		ID:      "orun.integrations/reconcile@v1",
		Summary: "Create the declared brokered secrets that do not exist yet, minted from the workspace's connections",
		Doing:   "Minting this phase's secrets from your connections.",
		Params: append(orgParams(),
			Param{Name: "provider", Type: ParamString, Required: true,
				Description: "the connected provider the secrets are minted from"},
			Param{Name: "keys", Type: ParamStringList,
				Description: "secret keys to ensure, all minted under `template`"},
			Param{Name: "template", Type: ParamString,
				Description: "the broker scope template each of `keys` is minted under"},
			Param{Name: "secrets", Type: ParamObjectList,
				Description: "secrets to ensure, each `{key, template}` — several templates in one hook; the missing ones are created side by side"},
			Param{Name: "project", Type: ParamString,
				Description: "project scope; empty uses the workspace scope"},
			Param{Name: "connection", Type: ParamString,
				Description: "which connection to mint from when the provider has several: its id, display name or account login; empty uses the connection the build is bound to (ORUN_CONNECTIONS), else the first active one"},
		),
		Outputs: []string{"created", "kept", "connection"},
		Did:     reconcileDid,
		Check:   checkReconcileWith,
	}, runIntegrationsReconcile)
}

// wantedSecret is one key a reconcile ensures, and the template it is minted
// under.
type wantedSecret struct {
	Key      string
	Template string
}

// checkReconcileWith takes one of the two forms, never both: `secrets`, or
// `keys` with their `template`.
func checkReconcileWith(with map[string]any) error {
	hasSecrets := with["secrets"] != nil
	hasKeys := with["keys"] != nil
	switch {
	case hasSecrets && (hasKeys || with["template"] != nil):
		return fmt.Errorf("takes \"secrets\" or \"keys\" with \"template\", not both")
	case hasSecrets:
		items, _ := with["secrets"].([]any)
		for i, item := range items {
			m, _ := item.(map[string]any)
			key, _ := m["key"].(string)
			template, _ := m["template"].(string)
			if strings.TrimSpace(key) == "" || strings.TrimSpace(template) == "" {
				return fmt.Errorf("parameter \"secrets\" item %d needs both \"key\" and \"template\"", i)
			}
		}
		return nil
	case hasKeys && with["template"] == nil:
		return fmt.Errorf("requires parameter \"template\" with \"keys\" (the broker scope template they are minted under)")
	case !hasKeys:
		return fmt.Errorf("requires parameter \"secrets\" (each {key, template}) or \"keys\" with \"template\"")
	}
	return nil
}

// wantedSecrets is the hook's secrets in declaration order, from either form,
// each key once (the first declaration wins).
func wantedSecrets(in Input) []wantedSecret {
	var out []wantedSecret
	seen := map[string]bool{}
	add := func(key, template string) {
		key, template = strings.TrimSpace(key), strings.TrimSpace(template)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, wantedSecret{Key: key, Template: template})
	}
	if items, ok := in.Params["secrets"].([]any); ok {
		for _, item := range items {
			m, _ := item.(map[string]any)
			key, _ := m["key"].(string)
			template, _ := m["template"].(string)
			add(key, template)
		}
	}
	template := StringParam(in, "template")
	for _, key := range StringListParam(in, "keys") {
		add(key, template)
	}
	return out
}

func runIntegrationsReconcile(ctx context.Context, in Input) (Result, error) {
	client, org, err := configClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	// The run memo (run_memo.go): a phase's secrets hooks read the same
	// connections and the same secrets one after another.
	return reconcileOn(ctx, memoSecretsPlane{secretsPlane: client, memo: buildMemo}, org, in)
}

// connectionsRefresher reads the connections from the platform rather than
// from the run memo — what a reconcile reads again before it waits for one.
type connectionsRefresher interface {
	RefreshConnections(ctx context.Context, org string) ([]configsurface.Connection, error)
}

func reconcileOn(ctx context.Context, c secretsPlane, org string, in Input) (Result, error) {
	provider := strings.ToLower(strings.TrimSpace(StringParam(in, "provider")))
	wanted := wantedSecrets(in)
	scope := secretScope(org, in)

	conns, err := c.ListConnections(ctx, org)
	if err != nil {
		return Result{}, fmt.Errorf("reading connections for %s: %w", org, err)
	}
	// The connection to mint from: the hook's own pin, else the one the BUILD
	// is bound to. A console-resolved bootstrap binds one connection per
	// provider, and every fact the build was handed (the Cloudflare account's
	// workers.dev subdomain baked into the product) was read from it. Minting
	// from "the first active one" instead put a product's Workers on one
	// account while its files named another's subdomain, whenever the
	// workspace could use two Cloudflare accounts. A bound connection is
	// treated exactly like a pinned one: it must match, and no match waits.
	ref := strings.TrimSpace(StringParam(in, "connection"))
	if ref == "" {
		ref = boundConnectionFromEnv(provider)
	}
	connectionID, wait, err := chooseConnection(conns, provider, ref, org)
	if err != nil {
		return Result{}, err
	}
	// A wait is decided on a fresh read: the connections may be the run's
	// remembered answer, and a person may have connected the provider since.
	if wait != nil {
		if r, ok := c.(connectionsRefresher); ok {
			if conns, err = r.RefreshConnections(ctx, org); err != nil {
				return Result{}, fmt.Errorf("reading connections for %s: %w", org, err)
			}
			if connectionID, wait, err = chooseConnection(conns, provider, ref, org); err != nil {
				return Result{}, err
			}
		}
	}
	if wait != nil {
		return Result{Outputs: map[string]string{"created": "", "kept": "", "connection": ""}, Pending: wait}, nil
	}

	existing, _, err := c.ListSecrets(ctx, scope, true)
	if err != nil {
		return Result{}, fmt.Errorf("reading secrets for %s: %w", org, err)
	}
	have := map[string]bool{}
	for _, s := range existing {
		have[s.SecretKey] = true
	}

	var kept []string
	var missing []wantedSecret
	for _, w := range wanted {
		if have[w.Key] {
			kept = append(kept, w.Key)
			continue
		}
		missing = append(missing, w)
	}
	created, failedKey, err := createSecrets(ctx, c, scope, connectionID, missing)
	sort.Strings(created)
	sort.Strings(kept)
	outputs := map[string]string{
		"created": strings.Join(created, ","), "kept": strings.Join(kept, ","), "connection": connectionID,
	}
	if err != nil {
		// Resource-hiding masks authorization as not-found: when the READS
		// above worked and the WRITE does not, the credential is almost
		// certainly below the admin floor. Say so, because the raw error
		// sends people hunting for a missing scope.
		return Result{Outputs: outputs}, fmt.Errorf(
			"creating %s failed. The reads above succeeded, so if this is a not-found, this credential's role is below ADMIN — brokered secret creation requires an admin-role key. Underlying error: %w", failedKey, err)
	}
	return Result{Outputs: outputs}, nil
}

// chooseConnection is the connection a reconcile mints from: the pinned or
// bound one (`ref`) when there is one, else the provider's first active
// connection. No match is a wait, not a failure — a consent a person has not
// clicked is not a broken build — and a ref two connections share is refused
// rather than guessed.
func chooseConnection(conns []configsurface.Connection, provider, ref, org string) (string, *Pending, error) {
	if ref != "" {
		// MCX-D7: a pinned connection.
		matched := matchConnectionRef(conns, provider, ref)
		if len(matched) > 1 {
			return "", nil, fmt.Errorf("connection %q matches %d %s connections in workspace %s; pin one by id: %s",
				ref, len(matched), provider, org, connectionCandidates(conns, provider))
		}
		if len(matched) == 0 {
			return "", &Pending{Reason: fmt.Sprintf("no active %s connection %q in workspace %s (active: %s)",
				provider, ref, org, connectionCandidates(conns, provider))}, nil
		}
		return matched[0].ID, nil, nil
	}
	// Unpinned: the first active connection of the provider, exactly as
	// before references existed.
	for _, conn := range conns {
		if strings.EqualFold(conn.Provider, provider) && strings.EqualFold(conn.Status, "active") {
			return conn.ID, nil, nil
		}
	}
	return "", &Pending{Reason: fmt.Sprintf("%s is not connected in workspace %s", provider, org)}, nil
}

// createSecrets creates every missing secret side by side: each create is its
// own round trip to the platform, and nothing about one waits on another. It
// reports the keys created and, when any create failed, the first failure in
// declaration order — the others still ran, and a re-run keeps what they made.
func createSecrets(ctx context.Context, c secretsPlane, scope configsurface.Scope, connectionID string, missing []wantedSecret) ([]string, string, error) {
	errs := make([]error, len(missing))
	var wg sync.WaitGroup
	for i, w := range missing {
		wg.Add(1)
		go func(i int, w wantedSecret) {
			defer wg.Done()
			_, errs[i] = c.CreateSecret(ctx, scope, configsurface.CreateSecretRequest{
				SecretKey: w.Key,
				Binding:   &configsurface.SecretBrokerBinding{ConnectionID: connectionID, Template: w.Template},
			})
		}(i, w)
	}
	wg.Wait()
	var created []string
	failedKey, firstErr := "", error(nil)
	for i, w := range missing {
		if errs[i] != nil {
			if firstErr == nil {
				failedKey, firstErr = w.Key, errs[i]
			}
			continue
		}
		created = append(created, w.Key)
	}
	return created, failedKey, firstErr
}
