package actions

import (
	"fmt"
	"net/url"
	"strings"
)

// Facts and done lines (orun-cloud saas-build-panel BV3).
//
// A build's event stream says what the engine is DOING. A person watching
// an hour-long build also wants to keep in view what it has MADE: the
// repository, the epic, each pull request and where it stands, the
// addresses that answer. Those are facts: one record per thing, replaced
// when the thing changes (a pull request goes from Opening to Checks
// running to Merged), never a sentence a reader has to parse.
//
// The action knows what it made, so the action says it, from its own
// outputs. The engine scopes each fact to the hook that filed it, applies
// the hook's `label` / `pin` / `quiet`, and sends a fact only when it
// changed.
//
// The same is true of the line a finished step leaves behind. A hook may
// author one (`narrate:`); one that does not gets the action's `Did`
// sentence over its outputs, so a step that took ten minutes is never
// followed by silence. Bookkeeping (finding a task) has no Did and stays
// silent, as it has no Doing.

// Fact is one thing a build made, kept in view for a person.
type Fact struct {
	// Key is the fact's identity: a later fact with the same key replaces
	// it. Empty means "this hook's", and the engine scopes it to the phase
	// and hook that filed it.
	Key string
	// Kind is what it is: repo | epic | pr | url | run. A reader pins the
	// kinds it knows and keeps the rest as the build's record.
	Kind string
	// Label is what a reader calls it ("Console", the epic's name). May be
	// empty; the hook's `label:` overrides it.
	Label string
	// Value is the thing itself: owner/name, EP-3, #12, a URL, a run id.
	Value string
	// Href is where a reader goes to see it. May be empty.
	Href string
	// Env is a live address's environment (stage, prod).
	Env string
	// Status is where it stands. repo: created | found · pr: opening |
	// checks | failing | open | merged | closed · url: answered |
	// unreachable · run: running | success | failure | …
	Status string
	// Private is a repository's visibility, when the action knows it.
	Private *bool
	// Unpinned keeps the fact off a reader's pinned list — a health URL is
	// probed, and recorded, but it is not an address a person opens.
	Unpinned bool
}

// FactsFunc derives an action's facts from its resolved parameters and its
// outputs — whatever it has so far when it reports progress, or when it
// failed (failed is then true).
type FactsFunc func(params map[string]any, outputs map[string]string, failed bool) []Fact

// DidFunc is the short past-tense line a finished action leaves when its hook
// authored none. "" says nothing.
type DidFunc func(params map[string]any, outputs map[string]string) string

// Report hands the caller what the action has so far, while it is still
// running: a landing has opened its pull request and is waiting on CI; a
// watch has found its run. Safe when nobody listens. The map is copied, so
// the action may keep writing its own.
func (in Input) Report(outputs map[string]string) {
	if in.Progress == nil || len(outputs) == 0 {
		return
	}
	cp := make(map[string]string, len(outputs))
	for k, v := range outputs {
		cp[k] = v
	}
	in.Progress(cp)
}

// FactsOf is an action's facts for these parameters and outputs, or nil when
// the action files none. params are applied with the action's declared
// defaults first, so a fact can read a parameter the hook left out.
func FactsOf(id string, params map[string]any, outputs map[string]string, failed bool) []Fact {
	spec, ok := Lookup(id)
	if !ok || spec.Facts == nil {
		return nil
	}
	return spec.Facts(withDefaults(spec, params), outputs, failed)
}

// DidOf is an action's done line for these parameters and outputs, or "".
func DidOf(id string, params map[string]any, outputs map[string]string) string {
	spec, ok := Lookup(id)
	if !ok || spec.Did == nil {
		return ""
	}
	return strings.TrimSpace(spec.Did(withDefaults(spec, params), outputs))
}

// withDefaults is params with the spec's defaults where params has none. A
// value that does not coerce is kept as given: a fact reads it best-effort,
// and the action itself already ran (or failed) on the coerced value.
func withDefaults(spec Spec, params map[string]any) map[string]any {
	out := make(map[string]any, len(spec.Params))
	for _, p := range spec.Params {
		v, given := params[p.Name]
		if !given {
			if p.Default != nil {
				out[p.Name] = p.Default
			}
			continue
		}
		if c, err := coerceType(p, v); err == nil {
			v = c
		}
		out[p.Name] = v
	}
	return out
}

func paramString(params map[string]any, name string) string {
	s, _ := params[name].(string)
	return strings.TrimSpace(s)
}

func paramBool(params map[string]any, name string) (bool, bool) {
	b, ok := params[name].(bool)
	return b, ok
}

// ── The kinds the registry files ───────────────────────────────────────

// repoFacts: the repository, with its visibility when this build created it
// (a found repository's visibility is GitHub's to say, and was not asked).
func repoFacts(params map[string]any, out map[string]string, _ bool) []Fact {
	name := strings.TrimSpace(out["fullName"])
	if name == "" {
		return nil
	}
	f := Fact{Key: "repo", Kind: "repo", Value: name, Href: "https://github.com/" + name, Status: "found"}
	if out["created"] == "true" {
		f.Status = "created"
		if private, ok := paramBool(params, "private"); ok {
			f.Private = &private
		}
	}
	return []Fact{f}
}

func repoDid(_ map[string]any, out map[string]string) string {
	name := strings.TrimSpace(out["fullName"])
	switch {
	case name == "":
		return ""
	case out["created"] == "true":
		return fmt.Sprintf("Created the repository %s.", name)
	default:
		return fmt.Sprintf("The repository %s is ready.", name)
	}
}

// prFacts: the pull request a landing opened, from the moment it is being
// opened to the moment it is merged. A landing with nothing to land files
// none; one that failed before a pull request existed withdraws the row it
// had reported as opening.
func prFacts(params map[string]any, out map[string]string, failed bool) []Fact {
	number := strings.TrimSpace(out["number"])
	f := Fact{Kind: "pr", Href: strings.TrimSpace(out["url"])}
	if number != "" {
		f.Value = "#" + number
	}
	switch {
	case number == "" && failed:
		f.Status, f.Unpinned = "closed", true
	case number == "" && out["stage"] == "opening":
		f.Status = "opening"
	case number == "":
		return nil
	case out["merged"] == "true":
		f.Status = "merged"
	case failed:
		f.Status = "failing"
	default:
		if draft, _ := paramBool(params, "draft"); draft {
			f.Status = "open"
		} else {
			f.Status = "checks"
		}
	}
	return []Fact{f}
}

func prDid(params map[string]any, out map[string]string) string {
	number := strings.TrimSpace(out["number"])
	base := paramString(params, "base")
	if base == "" {
		base = "main"
	}
	switch {
	case number == "" && out["landed"] == "false":
		return fmt.Sprintf("There was nothing to land: %s already has every file.", base)
	case number == "":
		return ""
	case out["merged"] == "true" && out["checked"] == "local":
		return fmt.Sprintf("Checked here and merged #%s into %s.", number, base)
	case out["merged"] == "true" && out["checked"] == "local+ci":
		return fmt.Sprintf("Checked here, waited for the checks %s requires, and merged #%s.", base, number)
	case out["merged"] == "true":
		return fmt.Sprintf("Merged #%s into %s.", number, base)
	default:
		return fmt.Sprintf("Opened #%s.", number)
	}
}

// runFacts: the convergence run a watch found, kept as the build's record —
// it is a link a failure points at, not a row a reader keeps in view.
func runFacts(_ map[string]any, out map[string]string, failed bool) []Fact {
	id := strings.TrimSpace(out["runId"])
	if id == "" || id == "0" {
		return nil
	}
	status := strings.TrimSpace(out["conclusion"])
	switch {
	case failed && status == "":
		status = "failure"
	case status == "":
		status = "running"
	}
	return []Fact{{Kind: "run", Value: id, Href: strings.TrimSpace(out["url"]), Status: status, Unpinned: true}}
}

func runDid(params map[string]any, out map[string]string) string {
	branch := paramString(params, "branch")
	if branch == "" {
		branch = "main"
	}
	switch out["conclusion"] {
	case "success":
		if n := strings.TrimSpace(out["resumes"]); n != "" && n != "0" {
			return fmt.Sprintf("The convergence run on %s is green, after %s resume(s).", branch, n)
		}
		return fmt.Sprintf("The convergence run on %s is green.", branch)
	case "none":
		return fmt.Sprintf("There was no convergence run on %s to watch.", branch)
	}
	return ""
}

// probeFacts: each address a probe checked, labelled by its environment.
// A health check is probed and recorded but not pinned: a person opens the
// console, not its /health.
func probeFacts(params map[string]any, out map[string]string, _ bool) []Fact {
	failed := map[string]bool{}
	for _, u := range splitLines(out["failedUrls"]) {
		failed[u] = true
	}
	byURL := map[string]ProbeTarget{}
	for _, t := range ProbeTargets(params) {
		byURL[t.URL] = t
	}
	var facts []Fact
	for _, u := range splitLines(out["urls"]) {
		t := byURL[u]
		f := Fact{Key: "url:" + u, Kind: "url", Value: u, Href: u, Label: t.Label, Env: t.Env, Status: "answered"}
		if f.Env == "" {
			f.Env = EnvOfURL(u)
		}
		if failed[u] {
			f.Status = "unreachable"
		}
		switch {
		case t.Pin != nil:
			f.Unpinned = !*t.Pin
		default:
			f.Unpinned = IsHealthURL(u)
		}
		facts = append(facts, f)
	}
	return facts
}

func probeDid(_ map[string]any, out map[string]string) string {
	n := len(splitLines(out["urls"]))
	switch {
	case n == 0:
		return ""
	case n == 1:
		return "The address answered."
	default:
		return fmt.Sprintf("All %d addresses answered.", n)
	}
}

// epicFacts: the epic a build files its work under. Milestones and tasks are
// bookkeeping a reader finds on the epic.
func epicFacts(params map[string]any, out map[string]string, _ bool) []Fact {
	if paramString(params, "kind") != "epic" {
		return nil
	}
	key := strings.TrimSpace(out["key"])
	if key == "" {
		return nil
	}
	status := "created"
	if out["existed"] == "true" {
		status = "found"
	}
	return []Fact{{Key: "epic", Kind: "epic", Value: key, Label: paramString(params, "name"), Status: status}}
}

func epicDid(params map[string]any, out map[string]string) string {
	key := strings.TrimSpace(out["key"])
	if paramString(params, "kind") != "epic" || key == "" {
		return ""
	}
	if out["existed"] == "true" {
		return fmt.Sprintf("Found the epic %s.", key)
	}
	return fmt.Sprintf("Opened the epic %s.", key)
}

func reconcileDid(_ map[string]any, out map[string]string) string {
	created, kept := len(splitList(out["created"])), len(splitList(out["kept"]))
	switch {
	case created == 0 && kept == 0:
		return ""
	case created == 0:
		return fmt.Sprintf("Every secret this phase needs was already there (%d).", kept)
	case kept == 0:
		return fmt.Sprintf("Minted %d secret(s) from your connection.", created)
	default:
		return fmt.Sprintf("Minted %d secret(s) from your connection; %d were already there.", created, kept)
	}
}

// ── Addresses ──────────────────────────────────────────────────────────

// envWords are the environment names a deployed address carries in its host
// (`notes-console-stage.acme.workers.dev`, `stage.notes.dev`).
var envWords = map[string]bool{
	"prod": true, "production": true, "stage": true, "staging": true,
	"dev": true, "preview": true, "qa": true, "uat": true,
}

// EnvOfURL is the environment an address's host names, or "". The LAST
// matching word wins, which is the one a naming scheme appends
// (`api-dev-tools-stage` is stage). The top-level domain never counts:
// every `*.workers.dev` address would otherwise be "dev".
func EnvOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	labels := strings.Split(strings.ToLower(u.Hostname()), ".")
	if len(labels) > 1 {
		labels = labels[:len(labels)-1]
	}
	found := ""
	for _, label := range labels {
		for _, word := range strings.Split(label, "-") {
			if envWords[word] {
				found = word
			}
		}
	}
	return found
}

// healthPaths are the paths an address answers a machine on, not a person.
var healthPaths = map[string]bool{
	"/health": true, "/healthz": true, "/ready": true, "/readyz": true,
	"/livez": true, "/status": true, "/ping": true,
}

// IsHealthURL reports whether an address is a health check.
func IsHealthURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	p := strings.ToLower(strings.TrimRight(u.Path, "/"))
	if healthPaths[p] {
		return true
	}
	for hp := range healthPaths {
		if strings.HasSuffix(p, hp) {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
