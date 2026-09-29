package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// saas-work-gitops WG3: the sync creates what the tree declares and the
// platform lacks, updates what differs, never deletes, and is silent on a
// second run. A fake platform records every write with its idempotency key
// and the X-Orun-Work-Sync header.

type fakePlatform struct {
	mu         sync.Mutex
	t          *testing.T
	epic       map[string]any // nil = not found
	milestones []map[string]any
	tasks      map[string]map[string]any
	docs       map[string]string
	writes     []string // "METHOD path idem header"
	// stampsOnly counts the empty PATCHes: the sync's stamp, no field.
	stampsOnly int
}

func (p *fakePlatform) record(r *http.Request) {
	p.writes = append(p.writes, r.Method+" "+r.URL.Path+" "+r.Header.Get("Idempotency-Key")+" "+r.Header.Get("X-Orun-Work-Sync"))
}

func (p *fakePlatform) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/v1/organizations/org_x/tasks")
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch {
		case r.Method == "GET" && path == "/epics/saas-work-gitops":
			if p.epic == nil {
				envelope(p.t, w, 404, map[string]any{"error": "not_found"})
				return
			}
			envelope(p.t, w, 200, map[string]any{"epic": p.epic, "milestones": p.milestones, "taskCount": len(p.tasks)})
		case r.Method == "POST" && path == "/epics":
			p.record(r)
			p.epic = map[string]any{"id": "epc_1", "slug": body["slug"], "name": body["name"], "description": body["description"], "state": "Planning", "stateCategory": "backlog", "owner": "usr_1"}
			p.stamp(r)
			envelope(p.t, w, 201, map[string]any{"epic": p.epic})
		case r.Method == "PATCH" && path == "/epics/saas-work-gitops":
			p.record(r)
			if len(body) == 0 {
				p.stampsOnly++
			}
			p.stamp(r)
			for k, v := range body {
				if k == "state" {
					p.epic["stateCategory"] = v
				}
				p.epic[k] = v
			}
			envelope(p.t, w, 200, map[string]any{"epic": p.epic})
		case r.Method == "POST" && path == "/epics/saas-work-gitops/milestones":
			p.record(r)
			m := map[string]any{"id": "mls_" + strings.ReplaceAll(body["name"].(string), " ", "_"), "epicId": "epc_1", "name": body["name"], "sortOrder": float64(len(p.milestones) + 1), "exitCriteria": body["exitCriteria"]}
			p.milestones = append(p.milestones, m)
			envelope(p.t, w, 201, map[string]any{"milestone": m})
		case r.Method == "PATCH" && strings.HasPrefix(path, "/milestones/"):
			p.record(r)
			envelope(p.t, w, 200, map[string]any{"milestone": map[string]any{"id": strings.TrimPrefix(path, "/milestones/")}})
		case r.Method == "GET" && strings.HasPrefix(path, "/") && !strings.Contains(path, "/epics") && !strings.Contains(path, "/milestones"):
			key := strings.TrimPrefix(path, "/")
			if tk, ok := p.tasks[key]; ok {
				envelope(p.t, w, 200, map[string]any{"task": tk})
				return
			}
			envelope(p.t, w, 404, map[string]any{"error": "not_found"})
		case r.Method == "POST" && path == "":
			p.record(r)
			key := body["adoptKey"].(string)
			p.tasks[key] = map[string]any{"id": "tsk_" + key, "key": key, "titleMirror": body["titleMirror"], "milestone": map[string]any{"id": body["milestone"]}}
			envelope(p.t, w, 201, map[string]any{"task": p.tasks[key]})
		case r.Method == "PUT" && strings.HasSuffix(path, "/contract"):
			p.record(r)
			key := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/contract")
			p.tasks[key]["contractHash"] = body["contractHash"]
			envelope(p.t, w, 200, map[string]any{"contractHash": body["contractHash"], "syncedAt": "2026-01-01T00:00:00Z"})
		case r.Method == "PUT" && strings.Contains(path, "/docs/"):
			p.record(r)
			content := body["content"].(string)
			updated := p.docs[path] != content
			p.docs[path] = content
			envelope(p.t, w, 200, map[string]any{"updated": updated, "contentHash": "sha256:doc"})
		default:
			p.t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			envelope(p.t, w, 500, nil)
		}
	}
}

// stamp does what the platform does under the sync header: the epic carries
// the pointer at the commit the header names.
func (p *fakePlatform) stamp(r *http.Request) {
	if h := r.Header.Get("X-Orun-Work-Sync"); h != "" {
		repo, sha, _ := strings.Cut(h, "@")
		p.epic["managedBy"] = map[string]any{"repo": repo, "path": "work/epics/saas-work-gitops/epic.yaml", "sha": sha, "keyPrefix": "WG", "syncedAt": "2026-01-01T00:00:00Z"}
	}
}

func writeSyncRepo(t *testing.T) string {
	dir := writeWorkRepo(t)
	// on-merge, and a design doc beside the declaration
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\nwork:\n  epics: work/epics\n  tasks: work/tasks\n  sync: on-merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work", "epics", "saas-work-gitops", "design.md"), []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "declare")
	return dir
}

func TestWorkSyncCreatesThenIsIdempotent(t *testing.T) {
	writeSyncRepo(t)
	p := &fakePlatform{t: t, tasks: map[string]map[string]any{}, docs: map[string]string{}}
	srv := httptest.NewServer(p.handler())
	defer srv.Close()

	// dry run: the plan, no writes
	out, err := runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--dry-run", "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if len(p.writes) != 0 || !strings.Contains(out, "would create   epic saas-work-gitops") || !strings.Contains(out, "would create   task WG-1") {
		t.Fatalf("dry run wrote or did not plan:\n%s\n%v", out, p.writes)
	}

	// first run: everything is created, in order
	out, err = runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	joined := strings.Join(p.writes, "\n")
	for _, want := range []string{
		"POST /v1/organizations/org_x/tasks/epics work:sourceplane/t:work/epics/saas-work-gitops/epic.yaml:",
		"POST /v1/organizations/org_x/tasks/epics/saas-work-gitops/milestones",
		"POST /v1/organizations/org_x/tasks work:sourceplane/t:work/tasks/WG-1.TaskContract.yaml:",
		"PUT /v1/organizations/org_x/tasks/WG-1/contract",
		"PUT /v1/organizations/org_x/tasks/WG-2/contract",
		"PUT /v1/organizations/org_x/tasks/epics/saas-work-gitops/docs/design",
		"PUT /v1/organizations/org_x/tasks/epics/saas-work-gitops/docs/implementation-status",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing write %q in:\n%s", want, joined)
		}
	}
	for _, w := range p.writes {
		if !strings.HasSuffix(w, " sourceplane/t@"+headSha(t)) {
			t.Errorf("write without the sync header: %s", w)
		}
	}
	// create carries no state, so the declared `started` is the one PATCH
	if n := strings.Count(joined, "PATCH"); n != 1 || !strings.Contains(joined, "PATCH /v1/organizations/org_x/tasks/epics/saas-work-gitops ") {
		t.Errorf("a fresh epic needs exactly the state PATCH:\n%s", joined)
	}
	if !strings.Contains(out, "state → started") {
		t.Errorf("state not applied:\n%s", out)
	}

	// second run: nothing to write. Docs are re-sent (the server is the
	// content-hash idempotency, and answers updated=false); every other
	// write is a comparison here first.
	before := len(p.writes)
	out, err = runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("second sync: %v\n%s", err, out)
	}
	for _, w := range p.writes[before:] {
		if !strings.Contains(w, "/docs/") {
			t.Errorf("second run wrote: %s", w)
		}
	}
	if !strings.Contains(out, "0 write(s)") || !strings.Contains(out, "skip           doc design — unchanged") {
		t.Fatalf("second run:\n%s", out)
	}
}

func TestWorkSyncNeverMovesBackwardsAndNeverDeletes(t *testing.T) {
	writeSyncRepo(t)
	p := &fakePlatform{t: t, tasks: map[string]map[string]any{}, docs: map[string]string{}}
	p.epic = map[string]any{"id": "epc_1", "slug": "saas-work-gitops", "name": "Work as code", "stateCategory": "completed", "owner": "usr_1"}
	p.milestones = []map[string]any{
		{"id": "mls_old", "epicId": "epc_1", "name": "Forgotten phase", "sortOrder": 1.0},
		{"id": "mls_WG0", "epicId": "epc_1", "name": "WG0", "sortOrder": 2.0},
		{"id": "mls_WG1", "epicId": "epc_1", "name": "WG1", "sortOrder": 3.0},
	}
	p.tasks["WG-1"] = map[string]any{"id": "tsk_1", "key": "WG-1", "milestone": map[string]any{"id": "mls_WG0"}, "contractHash": "sha256:stale"}
	p.tasks["WG-2"] = map[string]any{"id": "tsk_2", "key": "WG-2", "milestone": map[string]any{"id": "mls_old"}, "contractHash": ""}
	srv := httptest.NewServer(p.handler())
	defer srv.Close()
	out, err := runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	for _, want := range []string{
		"the platform says completed, a terminal state",
		`milestone "Forgotten phase" — exists on the platform but work/epics/saas-work-gitops/epic.yaml does not declare it`,
		`milestone "WG0" — position`, // first declared, but second on the platform
		"task WG-2 — sits in mls_old on the platform",
		"attach         contract WG-1",
		"attach         contract WG-2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	joined := strings.Join(p.writes, "\n")
	if strings.Contains(joined, "DELETE") || strings.Contains(joined, "POST /v1/organizations/org_x/tasks/epics") {
		t.Errorf("deleted or re-created:\n%s", joined)
	}
}

func TestWorkSyncRefusesWhenOffOrBroken(t *testing.T) {
	dir := writeWorkRepo(t) // sync: absent → off
	out, err := runTaskCmd(t, newWorkSyncCommand(), "", "--repo", "sourceplane/t")
	if err == nil || !strings.Contains(err.Error(), "work.sync is \"off\"") {
		t.Fatalf("off must refuse: %v\n%s", err, out)
	}
	// a broken tree refuses even with --force, before any client is built
	if err := os.WriteFile(filepath.Join(dir, "work", "tasks", "WG-9.TaskContract.yaml"), []byte(strings.Replace(testTaskDoc, "name: ENG-1", "name: WG-9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runTaskCmd(t, newWorkSyncCommand(), "", "--force", "--repo", "sourceplane/t")
	if err == nil || !strings.Contains(err.Error(), "1 problem(s)") || !strings.Contains(out, "WG-9 carries the prefix WG") {
		t.Fatalf("broken tree must refuse: %v\n%s", err, out)
	}
}

func headSha(t *testing.T) string {
	t.Helper()
	sha, err := gitOutIn(".", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// The edge accepts only printable ASCII in Idempotency-Key; milestone names
// and change lists carry dashes, arrows and spaces, so the WHAT is digested.
func TestWorkSyncIdempotencyKeysAreASCII(t *testing.T) {
	s := &workSyncer{repo: "sourceplane/t", sha: "abc123"}
	for _, what := range []string{"epic:state → started", "milestone:WG0 — the spec", "contract:sha256:ff"} {
		k := s.idem("work/epics/e/epic.yaml", what)
		if !strings.HasPrefix(k, "work:sourceplane/t:work/epics/e/epic.yaml:abc123:") {
			t.Fatalf("prefix lost: %s", k)
		}
		for _, r := range k {
			if r < 0x20 || r > 0x7e {
				t.Fatalf("non-ASCII in key %q", k)
			}
		}
	}
	if s.idem("p", "a") == s.idem("p", "b") {
		t.Fatal("different whats must not collide")
	}
}

// Design §6: every run moves the pointer. A commit that changes nothing the
// sync writes still stamps the epic — once, with an empty PATCH — and the
// same commit run twice stamps nothing.
func TestWorkSyncStampsAnUnchangedEpicOncePerCommit(t *testing.T) {
	dir := writeSyncRepo(t)
	p := &fakePlatform{t: t, tasks: map[string]map[string]any{}, docs: map[string]string{}}
	srv := httptest.NewServer(p.handler())
	defer srv.Close()

	if out, err := runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t"); err != nil {
		t.Fatalf("first sync: %v\n%s", err, out)
	}
	if p.stampsOnly != 0 {
		t.Fatalf("a run that created and updated the epic has nothing left to stamp; got %d empty PATCHes", p.stampsOnly)
	}

	// a commit that touches nothing the sync declares
	if err := os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "unrelated")
	before := len(p.writes)
	out, err := runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("second sync: %v\n%s", err, out)
	}
	var nonDoc []string
	for _, w := range p.writes[before:] {
		if !strings.Contains(w, "/docs/") {
			nonDoc = append(nonDoc, w)
		}
	}
	if len(nonDoc) != 1 || !strings.Contains(nonDoc[0], "PATCH /v1/organizations/org_x/tasks/epics/saas-work-gitops work:sourceplane/t:work/epics/saas-work-gitops/epic.yaml:"+headSha(t)) {
		t.Fatalf("expected exactly the stamp PATCH, got %v", nonDoc)
	}
	if p.stampsOnly != 1 || !strings.Contains(out, "stamp          epic saas-work-gitops — managedBy") || !strings.Contains(out, "1 write(s)") {
		t.Fatalf("stamp not reported as the one write:\n%s", out)
	}

	// the same commit again: the pointer already names it
	before = len(p.writes)
	out, err = runTaskCmd(t, newWorkSyncCommand(), srv.URL, "--repo", "sourceplane/t")
	if err != nil {
		t.Fatalf("third sync: %v\n%s", err, out)
	}
	for _, w := range p.writes[before:] {
		if !strings.Contains(w, "/docs/") {
			t.Errorf("third run wrote: %s", w)
		}
	}
	if p.stampsOnly != 1 || !strings.Contains(out, "0 write(s)") {
		t.Fatalf("third run:\n%s", out)
	}
}
