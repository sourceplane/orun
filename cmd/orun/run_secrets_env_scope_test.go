package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/secretref"
)

// envSecretStore is what the platform holds for one project: environment slug
// → secret KEY → value. Two environments may hold the same KEY — every
// cloudflare-d1 root publishes its wiring document as WIRING_CLOUDFLARE_D1 to
// its own environment.
type envSecretStore map[string]map[string]string

// fakeResolvePlatform answers POST …/secrets/resolve the way state-worker does
// since SE5-multi (orun-cloud #688): refs group BY ENVIRONMENT in the order
// they first appear, each group resolves on its own, the flat `secrets` map
// merges the groups in that order (a key served in two environments keeps the
// LAST group's value), and `secretsByEnv` (slug → KEY → value) carries every
// group separately. envAware=false drops `secretsByEnv`: a backend from
// before #688 that answers only the flat map.
func fakeResolvePlatform(t *testing.T, store envSecretStore, envAware bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Refs         []string `json:"refs"`
			OptionalRefs []string `json:"optionalRefs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode resolve body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		type want struct {
			key      string
			optional bool
		}
		var order []string
		groups := map[string][]want{}
		add := func(raw string, optional bool) {
			ref, err := secretref.Parse(raw)
			if err != nil {
				t.Errorf("platform got an unparseable ref: %v", err)
				return
			}
			slug := strings.ToLower(ref.Env)
			if _, seen := groups[slug]; !seen {
				order = append(order, slug)
			}
			groups[slug] = append(groups[slug], want{key: ref.Key, optional: optional})
		}
		for _, raw := range body.Refs {
			add(raw, false)
		}
		for _, raw := range body.OptionalRefs {
			add(raw, true)
		}

		flat := map[string]string{}
		byEnv := map[string]map[string]string{}
		for _, slug := range order {
			served := map[string]string{}
			for _, w := range groups[slug] {
				value, ok := store[slug][w.key]
				if !ok {
					if w.optional {
						continue
					}
					t.Errorf("test store has no %s in %s", w.key, slug)
					continue
				}
				served[w.key] = value
			}
			byEnv[slug] = served
			for k, v := range served {
				flat[k] = v // last group wins — the legacy flat map
			}
		}
		payload := map[string]any{"secrets": flat, "ttlSeconds": 300}
		if envAware {
			payload["secretsByEnv"] = byEnv
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": payload,
			"meta": map[string]any{"requestId": "req_test"},
		})
	}))
}

func resolverFor(srv *httptest.Server) func(string, []model.PlanSecretRef) (map[string]string, error) {
	client := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("t"))
	return remoteSecretResolver(context.Background(), nil, client, nil, "1-1", "runner-1", os.Stderr, false)
}

// The store behind the d1-todos deploy: each cloudflare-d1 root published its
// own WIRING_CLOUDFLARE_D1; the account id is served in both environments.
func turso2EnvStore() envSecretStore {
	return envSecretStore{
		"stage": {
			"WIRING_CLOUDFLARE_D1":  `{"database_id":"stage-db","database_name":"stg-d1-todos-stage"}`,
			"CLOUDFLARE_ACCOUNT_ID": "acct",
		},
		"prod": {
			"WIRING_CLOUDFLARE_D1":  `{"database_id":"prod-db","database_name":"prod-d1-todos-prod"}`,
			"CLOUDFLARE_ACCOUNT_ID": "acct",
		},
	}
}

// tursoWebJobRefs is the web deploy job's secret references exactly as the plan
// renders them (sorted by env var name): the profile's own-environment binding
// first, then the component's secretEnv reading BOTH rungs' wiring documents
// (turso-starter apps/web/component.yaml). The job's OWN environment is
// therefore the first environment group of the resolve, and the other one
// the last.
func tursoWebJobRefs(jobEnv string) []model.PlanSecretRef {
	return []model.PlanSecretRef{
		{AsEnv: "CLOUDFLARE_ACCOUNT_ID", Ref: "secret://turso-starter/turso-starter/" + jobEnv + "/CLOUDFLARE_ACCOUNT_ID"},
		{AsEnv: "WIRING_CLOUDFLARE_D1_PROD", Ref: "secret://turso-starter/turso-starter/prod/WIRING_CLOUDFLARE_D1"},
		{AsEnv: "WIRING_CLOUDFLARE_D1_STAGE", Ref: "secret://turso-starter/turso-starter/stage/WIRING_CLOUDFLARE_D1"},
	}
}

// assertEachRefGotItsOwnEnvironment is the invariant: every injected value is
// the one stored in the environment ITS reference names — never another
// environment's value for the same KEY.
func assertEachRefGotItsOwnEnvironment(t *testing.T, store envSecretStore, refs []model.PlanSecretRef, out map[string]string) {
	t.Helper()
	for _, ref := range refs {
		parsed, err := secretref.Parse(ref.Ref)
		if err != nil {
			t.Fatalf("parse %s: %v", ref.AsEnv, err)
		}
		want := store[strings.ToLower(parsed.Env)][parsed.Key]
		got, ok := out[ref.AsEnv]
		if !ok {
			t.Errorf("%s (env %s): no value injected", ref.AsEnv, parsed.Env)
			continue
		}
		if got != want {
			t.Errorf("%s names env %q but was injected %q, want %q", ref.AsEnv, parsed.Env, got, want)
		}
	}
}

// Regression (d1-todos run 3DZ68KFMH9X2KDSDDTJXCDR4Z6): web.stage bound prod's
// D1 database and web.prod bound stage's. Both wiring refs share the KEY
// WIRING_CLOUDFLARE_D1, the resolver looked each one up in the flat KEY-keyed
// map, and that map holds only the last environment group's document — so
// both env vars got the OTHER environment's document, in both jobs.
func TestRemoteSecretResolver_SameKeyInTwoEnvironmentsGetsEachItsOwnValue(t *testing.T) {
	store := turso2EnvStore()
	srv := fakeResolvePlatform(t, store, true)
	defer srv.Close()
	resolve := resolverFor(srv)

	for _, jobEnv := range []string{"stage", "prod"} {
		t.Run(jobEnv, func(t *testing.T) {
			refs := tursoWebJobRefs(jobEnv)
			out, err := resolve("web."+jobEnv+".verify-deploy", refs)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			assertEachRefGotItsOwnEnvironment(t, store, refs, out)
			if out["WIRING_CLOUDFLARE_D1_STAGE"] == out["WIRING_CLOUDFLARE_D1_PROD"] {
				t.Errorf("refs that differ only by environment share one value: %q", out["WIRING_CLOUDFLARE_D1_STAGE"])
			}
		})
	}

	// Ref order must not matter either: with no own-environment binding ahead
	// of them, the wiring refs alone make prod the first group.
	refs := tursoWebJobRefs("stage")[1:]
	out, err := resolve("web.stage.verify-deploy", refs)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertEachRefGotItsOwnEnvironment(t, store, refs, out)
}

// A backend that answers only the flat KEY-keyed map cannot say which
// environment a value came from. When a job asks for one KEY in two
// environments, injecting that one value under both names is exactly the
// cross-wire — the resolver must refuse instead.
func TestRemoteSecretResolver_FlatOnlyBackendRefusesAKeyAskedForInTwoEnvironments(t *testing.T) {
	store := turso2EnvStore()
	srv := fakeResolvePlatform(t, store, false)
	defer srv.Close()
	resolve := resolverFor(srv)

	out, err := resolve("web.stage.verify-deploy", tursoWebJobRefs("stage"))
	if err == nil {
		t.Fatalf("expected a refusal, got values for %d env var(s)", len(out))
	}
	for _, want := range []string{"WIRING_CLOUDFLARE_D1", "prod", "stage"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q: %v", want, err)
		}
	}
	for _, v := range store {
		for _, secret := range v {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error leaks a value: %v", err)
			}
		}
	}

	// A single-environment job is unambiguous and still resolves from the
	// flat map (version skew with an older backend stays green).
	single := []model.PlanSecretRef{
		{AsEnv: "CLOUDFLARE_ACCOUNT_ID", Ref: "secret://turso-starter/turso-starter/prod/CLOUDFLARE_ACCOUNT_ID"},
		{AsEnv: "WIRING_CLOUDFLARE_D1", Ref: "secret://turso-starter/turso-starter/prod/WIRING_CLOUDFLARE_D1"},
	}
	got, err := resolve("db-migrate.prod.migrate", single)
	if err != nil {
		t.Fatalf("single-environment resolve: %v", err)
	}
	assertEachRefGotItsOwnEnvironment(t, store, single, got)
}

// A best-effort ref whose KEY is not stored in ITS environment is skipped —
// never filled from another environment that does hold the KEY.
func TestRemoteSecretResolver_OptionalRefNeverBorrowsAnotherEnvironmentsValue(t *testing.T) {
	store := envSecretStore{
		"stage": {},
		"prod":  {"WIRING_CLOUDFLARE_KV": `{"namespace_id":"prod-kv"}`},
	}
	srv := fakeResolvePlatform(t, store, true)
	defer srv.Close()
	resolve := resolverFor(srv)

	out, err := resolve("web.stage.verify-deploy", []model.PlanSecretRef{
		{AsEnv: "WIRING_CLOUDFLARE_KV_PROD", Ref: "secret://ws/proj/prod/WIRING_CLOUDFLARE_KV"},
		{AsEnv: "WIRING_CLOUDFLARE_KV_STAGE", Ref: "secret://ws/proj/stage/WIRING_CLOUDFLARE_KV", Optional: true},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if v, ok := out["WIRING_CLOUDFLARE_KV_STAGE"]; ok {
		t.Errorf("the stage ref was filled with %q although stage holds no such key", v)
	}
	if out["WIRING_CLOUDFLARE_KV_PROD"] != store["prod"]["WIRING_CLOUDFLARE_KV"] {
		t.Errorf("prod ref: got %q", out["WIRING_CLOUDFLARE_KV_PROD"])
	}
}

// The platform matches environment slugs case-insensitively and answers under
// its own spelling; a ref spelled differently still finds its group.
func TestRemoteSecretResolver_EnvironmentGroupMatchesSlugCaseInsensitively(t *testing.T) {
	store := turso2EnvStore()
	srv := fakeResolvePlatform(t, store, true)
	defer srv.Close()
	resolve := resolverFor(srv)

	refs := []model.PlanSecretRef{
		{AsEnv: "WIRING_CLOUDFLARE_D1_PROD", Ref: "secret://ws/proj/prod/WIRING_CLOUDFLARE_D1"},
		{AsEnv: "WIRING_CLOUDFLARE_D1_STAGE", Ref: "secret://ws/proj/Stage/WIRING_CLOUDFLARE_D1"},
	}
	out, err := resolve("web.stage.verify-deploy", refs)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertEachRefGotItsOwnEnvironment(t, store, refs, out)
}
