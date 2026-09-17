package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/remotestate"
)

// orun.doctor/check@v1 and orun.integrations/reconcile@v1 and
// orun.secrets/exists@v1 (orun-bootstrap-engine BE-O5b).
//
// These three all speak to the same surface — the workspace's connections and
// its secrets — which is why they share a client here.

// configClient builds the connections/secrets client for an action, using the
// same narrow workspace resolution as the task-plane actions: the `org`
// parameter, then the environment (workspaceFromEnv), and nothing cleverer.
func configClient(ctx context.Context, in Input) (*configsurface.Client, string, error) {
	org := strings.TrimSpace(StringParam(in, "org"))
	if org == "" {
		org = workspaceFromEnv()
	}
	if org == "" {
		return nil, "", errNoWorkspace()
	}
	backend := backendURL(in)
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL: backend, Version: actionsVersion, Interactive: false, RequireLogin: true, Org: org,
	})
	if err != nil {
		return nil, "", fmt.Errorf("resolving a credential for %s: %w", org, err)
	}
	return configsurface.NewClient(backend, actionsVersion, tokenSrc), org, nil
}

// secretScope is the rung an action's secrets live on: the workspace, or a
// project when the hook names one.
//
// KIND IS NOT OPTIONAL. The client builds its request path by switching on it
// and rejects an empty one, so a Scope assembled without it names nothing:
//
//	✕ phase "04-workers" precondition "wiring" is not met: hook "wiring"
//	  (orun.secrets/exists@v1): reading secrets for ws_79BDXAZQ:
//	  configsurface: unknown scope kind ""
//
// Both actions that read or write secrets built one that way, so neither could
// ever have worked against a real backend. Their tests passed because the fake
// took the Scope and looked at nothing in it — true of the types, false of the
// values. The fakes now record it and the tests read it.
func secretScope(org string, in Input) configsurface.Scope {
	project := strings.TrimSpace(StringParam(in, "project"))
	if project == "" {
		return configsurface.Scope{Kind: configsurface.ScopeWorkspace, Org: org}
	}
	return configsurface.Scope{Kind: configsurface.ScopeProject, Org: org, Project: project}
}

// connections is the slice of the client these actions read, so the decision
// logic is testable without a backend.
type connections interface {
	ListConnections(ctx context.Context, org string) ([]configsurface.Connection, error)
}

// secretsLister reads a workspace's secret metadata. Never values: an action
// that checks a secret EXISTS has no business being able to read it.
type secretsLister interface {
	ListSecrets(ctx context.Context, scope configsurface.Scope, chain bool) ([]configsurface.SecretMeta, json.RawMessage, error)
}

// secretsPlane is what reconciling needs: read the connections, read what
// secrets exist, and create the ones that do not.
type secretsPlane interface {
	connections
	secretsLister
	CreateSecret(ctx context.Context, scope configsurface.Scope, req configsurface.CreateSecretRequest) (*configsurface.SecretMeta, error)
}

func init() {
	register(Spec{
		ID:      "orun.doctor/check@v1",
		Summary: "Require the named providers to be connected, waiting for a consent if asked",
		Params: append(orgParams(),
			Param{Name: "providers", Type: ParamStringList, Required: true,
				Description: "providers that must be connected and active"},
			Param{Name: "waitSeconds", Type: ParamInt, Default: 0,
				Description: "how long to wait for a missing consent; 0 reports pending immediately"},
			Param{Name: "pollSeconds", Type: ParamInt, Default: 20,
				Description: "how often to re-read the connections while waiting"},
		),
		Outputs: []string{"connected", "missing"},
	}, runDoctorCheck)

	register(Spec{
		ID:      "orun.secrets/exists@v1",
		Summary: "Require the named secret keys to exist in the workspace",
		Params: append(orgParams(),
			Param{Name: "keys", Type: ParamStringList, Required: true,
				Description: "secret keys that must already exist"},
			Param{Name: "project", Type: ParamString,
				Description: "project scope; empty reads the workspace scope"},
		),
		Outputs: []string{"present", "missing"},
	}, runSecretsExists)
}

func runDoctorCheck(ctx context.Context, in Input) (Result, error) {
	client, org, err := configClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	return doctorOn(ctx, client, org, in, time.Sleep)
}

// doctorOn is the decision, separated from the client so it is testable.
//
// A missing connection reports **pending**, not failure. Connecting a provider
// is a consent a person clicks in a console, and a bootstrap that treats "the
// human has not clicked yet" as a broken build tears itself down for being
// early. Waiting up to `waitSeconds` is how a baseline lets an operator click
// while preflight holds — that behaviour is currently ~40 lines of polling
// shell in every one of them.
func doctorOn(ctx context.Context, c connections, org string, in Input, sleep func(time.Duration)) (Result, error) {
	want := StringListParam(in, "providers")
	deadline := time.Now().Add(time.Duration(IntParam(in, "waitSeconds")) * time.Second)
	poll := time.Duration(IntParam(in, "pollSeconds")) * time.Second
	if poll <= 0 {
		poll = 20 * time.Second
	}
	for {
		live, err := c.ListConnections(ctx, org)
		if err != nil {
			// A failed READ is a real failure. Never mistake "the command did
			// not work" for "not connected yet" — that polls forever against a
			// broken credential, which is the failure this ordering prevents.
			return Result{}, fmt.Errorf("reading connections for %s: %w", org, err)
		}
		active := map[string]bool{}
		for _, conn := range live {
			if strings.EqualFold(conn.Status, "active") {
				active[strings.ToLower(conn.Provider)] = true
			}
		}
		var missing []string
		for _, p := range want {
			if !active[strings.ToLower(strings.TrimSpace(p))] {
				missing = append(missing, p)
			}
		}
		sort.Strings(missing)
		if len(missing) == 0 {
			return Result{Outputs: map[string]string{
				"connected": strings.Join(want, ","), "missing": "",
			}}, nil
		}
		if time.Now().After(deadline) {
			return Result{
				Outputs: map[string]string{"connected": "", "missing": strings.Join(missing, ",")},
				Pending: &Pending{
					Reason:     fmt.Sprintf("waiting for %s to be connected in workspace %s", strings.Join(missing, ", "), org),
					RetryAfter: poll,
				},
			}, nil
		}
		sleep(poll)
	}
}

func runSecretsExists(ctx context.Context, in Input) (Result, error) {
	client, org, err := configClient(ctx, in)
	if err != nil {
		return Result{}, err
	}
	return secretsExistOn(ctx, client, org, in)
}

// secretsExistOn checks that every named key is present.
//
// A missing secret is **pending**, not failure, for the same reason: the phase
// that publishes it may simply not have run yet, and a precondition that fails
// hard here would turn "run phase 03 first" into a broken bootstrap.
func secretsExistOn(ctx context.Context, c secretsLister, org string, in Input) (Result, error) {
	scope := secretScope(org, in)
	live, _, err := c.ListSecrets(ctx, scope, true)
	if err != nil {
		return Result{}, fmt.Errorf("reading secrets for %s: %w", org, err)
	}
	have := map[string]bool{}
	for _, s := range live {
		have[s.SecretKey] = true
	}
	var missing, present []string
	for _, k := range StringListParam(in, "keys") {
		if have[k] {
			present = append(present, k)
		} else {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(present)
	out := map[string]string{"present": strings.Join(present, ","), "missing": strings.Join(missing, ",")}
	if len(missing) > 0 {
		return Result{Outputs: out, Pending: &Pending{
			Reason: fmt.Sprintf("secret(s) not published yet: %s", strings.Join(missing, ", ")),
		}}, nil
	}
	return Result{Outputs: out}, nil
}
