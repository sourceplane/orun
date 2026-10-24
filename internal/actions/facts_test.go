package actions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Facts and done lines (orun-cloud saas-build-panel BV3): what each action
// says it made, and the line a finished step leaves when its hook authored
// none.

func TestAnAddressSaysItsEnvironmentFromItsHost(t *testing.T) {
	for url, want := range map[string]string{
		"https://notes-web-console-next-stage.acme.workers.dev":   "stage",
		"https://notes-api-edge-prod.acme.workers.dev/health":     "prod",
		"https://stage.notes.dev":                                 "stage",
		"https://notes-api-dev-tools-stage.acme.workers.dev":      "stage",
		"https://notes.dev":                                       "",
		"not a url":                                               "",
		"https://production-notes.example.com/dashboard?tab=prod": "production",
	} {
		if got := EnvOfURL(url); got != want {
			t.Errorf("EnvOfURL(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestAHealthCheckIsNotAnAddressAPersonOpens(t *testing.T) {
	for url, want := range map[string]bool{
		"https://a.workers.dev/health":       true,
		"https://a.workers.dev/api/healthz":  true,
		"https://a.workers.dev/ready/":       true,
		"https://a.workers.dev":              false,
		"https://a.workers.dev/order-status": false,
	} {
		if got := IsHealthURL(url); got != want {
			t.Errorf("IsHealthURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestARepositorySaysWhetherItWasMadeAndHowPrivate(t *testing.T) {
	created := FactsOf("orun.repo/ensure@v1", map[string]any{"owner": "acme", "name": "notes"},
		map[string]string{"fullName": "acme/notes", "created": "true"}, false)
	if len(created) != 1 {
		t.Fatalf("one repository fact, got %+v", created)
	}
	f := created[0]
	if f.Key != "repo" || f.Kind != "repo" || f.Value != "acme/notes" || f.Href != "https://github.com/acme/notes" || f.Status != "created" {
		t.Errorf("the created repository: %+v", f)
	}
	// `private` defaults to true, and a created repository took it.
	if f.Private == nil || !*f.Private {
		t.Errorf("a repository this build created is as private as it asked: %+v", f.Private)
	}
	found := FactsOf("orun.repo/ensure@v1", map[string]any{"private": false},
		map[string]string{"fullName": "acme/notes", "created": "false"}, false)
	if found[0].Status != "found" || found[0].Private != nil {
		t.Errorf("a found repository's visibility is GitHub's to say, not this build's: %+v", found[0])
	}
	if FactsOf("orun.repo/ensure@v1", nil, map[string]string{}, true) != nil {
		t.Error("no repository, no fact")
	}
}

func TestAPullRequestMovesFromOpeningToMerged(t *testing.T) {
	pr := func(out map[string]string, failed bool, params map[string]any) Fact {
		t.Helper()
		facts := FactsOf("orun.pr/land@v1", params, out, failed)
		if len(facts) != 1 {
			t.Fatalf("one pull request fact for %v, got %+v", out, facts)
		}
		return facts[0]
	}
	if f := pr(map[string]string{"stage": "opening"}, false, nil); f.Status != "opening" || f.Value != "" || f.Unpinned {
		t.Errorf("being opened: %+v", f)
	}
	open := map[string]string{"number": "12", "url": "https://github.com/acme/notes/pull/12", "merged": "false", "landed": "true"}
	if f := pr(open, false, nil); f.Status != "checks" || f.Value != "#12" || f.Href != open["url"] || f.Key != "" {
		t.Errorf("opened, its checks running — keyed by the engine, per hook: %+v", f)
	}
	if f := pr(open, false, map[string]any{"draft": true}); f.Status != "open" {
		t.Errorf("a draft is open, and nobody is merging it: %+v", f)
	}
	if f := pr(open, true, nil); f.Status != "failing" {
		t.Errorf("a landing that failed with its pull request open: %+v", f)
	}
	merged := map[string]string{"number": "12", "url": open["url"], "merged": "true", "landed": "true"}
	if f := pr(merged, false, nil); f.Status != "merged" {
		t.Errorf("merged: %+v", f)
	}
	// Failed before a pull request existed: the row it reported as opening
	// is withdrawn, not left spinning.
	if f := pr(map[string]string{}, true, nil); !f.Unpinned || f.Status != "closed" {
		t.Errorf("a landing that never opened its pull request: %+v", f)
	}
	nothing := map[string]string{"branch": "", "number": "", "url": "", "merged": "false", "landed": "false"}
	if facts := FactsOf("orun.pr/land@v1", nil, nothing, false); facts != nil {
		t.Errorf("nothing to land is no pull request: %+v", facts)
	}
}

func TestAProbeFilesEachAddressItChecked(t *testing.T) {
	params := map[string]any{
		"urls": []any{
			"https://notes-api-edge-stage.acme.workers.dev/health",
			"https://notes-console-stage.acme.workers.dev",
		},
		"targets": []any{
			map[string]any{"url": "https://notes.dev", "env": "prod", "label": "Console"},
			map[string]any{"url": "https://notes-console-stage.acme.workers.dev"}, // already listed
			map[string]any{"url": "https://notes-api.dev/ping", "pin": "true"},
		},
	}
	out := map[string]string{
		"urls":       "https://notes-api-edge-stage.acme.workers.dev/health\nhttps://notes-console-stage.acme.workers.dev\nhttps://notes.dev\nhttps://notes-api.dev/ping",
		"failedUrls": "https://notes.dev",
	}
	facts := FactsOf("orun.http/probe@v1", params, out, true)
	if len(facts) != 4 {
		t.Fatalf("one fact per address checked, got %+v", facts)
	}
	byURL := map[string]Fact{}
	for _, f := range facts {
		if f.Kind != "url" || f.Key != "url:"+f.Value || f.Href != f.Value {
			t.Errorf("an address is keyed and linked by itself: %+v", f)
		}
		byURL[f.Value] = f
	}
	if f := byURL["https://notes-api-edge-stage.acme.workers.dev/health"]; !f.Unpinned || f.Env != "stage" || f.Status != "answered" {
		t.Errorf("a health check is recorded, not pinned: %+v", f)
	}
	if f := byURL["https://notes-console-stage.acme.workers.dev"]; f.Unpinned || f.Env != "stage" {
		t.Errorf("the console, its env read off its host: %+v", f)
	}
	if f := byURL["https://notes.dev"]; f.Env != "prod" || f.Label != "Console" || f.Status != "unreachable" {
		t.Errorf("a labelled target that did not answer: %+v", f)
	}
	if f := byURL["https://notes-api.dev/ping"]; f.Unpinned {
		t.Errorf("a target's own pin outranks the health rule: %+v", f)
	}
}

func TestOnlyTheEpicIsAFactOfTheTaskPlane(t *testing.T) {
	epic := FactsOf("orun.task/ensure@v1", map[string]any{"kind": "epic", "name": "Infra baselining — Notes"},
		map[string]string{"id": "epc_1", "key": "EP-3", "existed": "false"}, false)
	if len(epic) != 1 || epic[0].Key != "epic" || epic[0].Value != "EP-3" || epic[0].Label != "Infra baselining — Notes" || epic[0].Status != "created" {
		t.Errorf("the epic: %+v", epic)
	}
	for _, kind := range []string{"milestone", "task"} {
		if f := FactsOf("orun.task/ensure@v1", map[string]any{"kind": kind}, map[string]string{"key": "ORUN-1"}, false); f != nil {
			t.Errorf("a %s is bookkeeping, found on the epic: %+v", kind, f)
		}
	}
}

func TestAConvergenceRunIsTheBuildsRecordNotARow(t *testing.T) {
	f := FactsOf("orun.run/watch@v1", nil, map[string]string{"runId": "77", "url": "https://github.com/acme/notes/actions/runs/77"}, false)
	if len(f) != 1 || f[0].Kind != "run" || !f[0].Unpinned || f[0].Status != "running" {
		t.Errorf("a run in flight: %+v", f)
	}
	if f := FactsOf("orun.run/watch@v1", nil, map[string]string{"runId": "0", "conclusion": "none"}, false); f != nil {
		t.Errorf("no run, no fact: %+v", f)
	}
}

func TestAFinishedStepSaysWhatItDid(t *testing.T) {
	for _, c := range []struct {
		id     string
		params map[string]any
		out    map[string]string
		want   string
	}{
		{"orun.repo/ensure@v1", nil, map[string]string{"fullName": "acme/notes", "created": "true"}, "Created the repository acme/notes."},
		{"orun.repo/ensure@v1", nil, map[string]string{"fullName": "acme/notes", "created": "false"}, "The repository acme/notes is ready."},
		{"orun.pr/land@v1", nil, map[string]string{"number": "12", "merged": "true"}, "Merged #12 into main."},
		{"orun.pr/land@v1", map[string]any{"base": "trunk"}, map[string]string{"number": "", "landed": "false"}, "There was nothing to land: trunk already has every file."},
		{"orun.pr/land@v1", map[string]any{"draft": true}, map[string]string{"number": "4", "merged": "false"}, "Opened #4."},
		{"orun.run/watch@v1", nil, map[string]string{"conclusion": "success", "resumes": "0"}, "The convergence run on main is green."},
		{"orun.run/watch@v1", nil, map[string]string{"conclusion": "success", "resumes": "2"}, "The convergence run on main is green, after 2 resume(s)."},
		{"orun.http/probe@v1", nil, map[string]string{"urls": "https://a\nhttps://b"}, "All 2 addresses answered."},
		{"orun.http/probe@v1", nil, map[string]string{"urls": "https://a"}, "The address answered."},
		{"orun.task/ensure@v1", map[string]any{"kind": "epic"}, map[string]string{"key": "EP-3", "existed": "false"}, "Opened the epic EP-3."},
		{"orun.task/ensure@v1", map[string]any{"kind": "epic"}, map[string]string{"key": "EP-3", "existed": "true"}, "Found the epic EP-3."},
		{"orun.task/ensure@v1", map[string]any{"kind": "task"}, map[string]string{"key": "ORUN-9", "existed": "false"}, ""},
		{"orun.integrations/reconcile@v1", nil, map[string]string{"created": "A,B", "kept": ""}, "Minted 2 secret(s) from your connection."},
		{"orun.integrations/reconcile@v1", nil, map[string]string{"created": "", "kept": "A"}, "Every secret this phase needs was already there (1)."},
		{"orun.task/rollup@v1", nil, map[string]string{"total": "3"}, ""},
	} {
		if got := DidOf(c.id, c.params, c.out); got != c.want {
			t.Errorf("DidOf(%s, %v) = %q, want %q", c.id, c.out, got, c.want)
		}
	}
}

func TestAProbeTakesUrlsOrTargetsButNotNeither(t *testing.T) {
	if err := Validate("orun.http/probe@v1", map[string]any{}); err == nil || !strings.Contains(err.Error(), "targets") {
		t.Errorf("a probe with nothing to probe is a blueprint error, got %v", err)
	}
	if err := Validate("orun.http/probe@v1", map[string]any{"targets": []any{map[string]any{"url": "https://a", "pin": false}}}); err != nil {
		t.Errorf("targets alone are enough: %v", err)
	}
	if err := Validate("orun.http/probe@v1", map[string]any{"targets": []any{map[string]any{"url": 3}}}); err == nil {
		t.Error("a target's values are strings or booleans")
	}
	if err := Validate("orun.http/probe@v1", map[string]any{"targets": []any{"https://a"}}); err == nil {
		t.Error("a target is a mapping")
	}
}

func TestAProbeReportsWhichAddressesItCheckedAndWhichFailed(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer ok.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	res, err := Run(context.Background(), "orun.http/probe@v1", Input{Params: map[string]any{
		"urls":    []any{ok.URL + "/health"},
		"targets": []any{map[string]any{"url": down.URL, "env": "stage"}},
	}})
	if err == nil {
		t.Fatal("a target that answered 503 fails the probe")
	}
	if res.Outputs["urls"] != ok.URL+"/health\n"+down.URL || res.Outputs["failedUrls"] != down.URL || res.Outputs["checked"] != "2" {
		t.Errorf("outputs: %v", res.Outputs)
	}
}

func TestProgressIsCopiedAndSafeWithNobodyListening(t *testing.T) {
	Input{}.Report(map[string]string{"number": "1"}) // nobody listening: nothing happens
	var got map[string]string
	in := Input{Progress: func(out map[string]string) { got = out }}
	out := map[string]string{"number": "1"}
	in.Report(out)
	out["number"] = "2"
	if got["number"] != "1" {
		t.Errorf("a report is a copy, so the action may keep writing its own map; got %v", got)
	}
}

// A landing says it is opening a pull request before the push, and which
// one as soon as it is open — before its checks, which can take an hour.
func TestALandingReportsItsPullRequestBeforeItMerges(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n"})
	params, err := Resolve("orun.pr/land@v1", map[string]any{
		"task": "BASE-1", "branchSlug": "scaffold", "title": "phase(scaffold)", "wait": false,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var reports []map[string]string
	res, err := landWith(context.Background(), r.pen, Input{Dir: r.product, Params: params,
		Progress: func(out map[string]string) { reports = append(reports, out) }})
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if len(reports) != 2 || reports[0]["stage"] != "opening" || reports[1]["number"] != res.Outputs["number"] || reports[1]["merged"] != "false" {
		t.Fatalf("reports: %v (final %v)", reports, res.Outputs)
	}
}
