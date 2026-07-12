package platformmcp

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

func readResource(t *testing.T, p *Provider, uri string) (string, error) {
	t.Helper()
	contents, owned, err := p.ReadResource(context.Background(), uri)
	if !owned {
		t.Fatalf("%s not owned by the platform provider", uri)
	}
	if err != nil {
		return "", err
	}
	if len(contents) != 1 || contents[0].URI != uri || contents[0].MimeType != resourceMimeType {
		t.Fatalf("contents shape: %+v", contents)
	}
	return contents[0].Text, nil
}

func TestResourceTemplates(t *testing.T) {
	templates := (&Provider{}).ResourceTemplates()
	if len(templates) != 2 {
		t.Fatalf("templates = %d, want 2", len(templates))
	}
	wantURIs := []string{"catalog://{workspace}/{entityKey}", "runs://{workspace}/{project}/{runId}"}
	for i, tpl := range templates {
		if tpl.URITemplate != wantURIs[i] {
			t.Errorf("template %d: uriTemplate = %q, want %q", i, tpl.URITemplate, wantURIs[i])
		}
		if tpl.MimeType != "text/markdown" {
			t.Errorf("%s: mimeType = %q, want text/markdown", tpl.Name, tpl.MimeType)
		}
	}
}

func TestEntityKeyCodec(t *testing.T) {
	key := EncodeEntityKey("component:default/api")
	if strings.ContainsAny(key, "+/=") {
		t.Fatalf("entityKey not base64url: %q", key)
	}
	ref, err := decodeEntityKey(key)
	if err != nil || ref != "component:default/api" {
		t.Fatalf("roundtrip = %q, %v", ref, err)
	}
	if _, err := decodeEntityKey("not!base64url"); err == nil || !strings.Contains(err.Error(), "validation_failed") {
		t.Fatalf("bad charset must be validation_failed, got %v", err)
	}
}

// TestCatalogEntityResourceRead: the full read path over the fake seam —
// list emulation with the exact-ref match, doc index, overview body — and
// the rendered markdown's identity/relations/provenance/docs/overview
// sections.
func TestCatalogEntityResourceRead(t *testing.T) {
	api := &fakeAPI{
		pages: []*remotestate.PlatformPage{
			page(`{"entities":[{"entityRef":"component:default/api-gateway","name":"gw"},{"entityRef":"component:default/api","name":"api","kind":"Component","owner":"team-a","lifecycle":"production","description":"the API","system":"core","relations":[{"type":"dependsOn","targetRef":"component:default/db"}],"sourceProjectId":"prj_1","sourceEnvironment":null,"sourceCommit":"abc123","headDigest":"sha256:head"}]}`, ""),
			page(`{"docs":[{"docKey":"overview","title":"Overview","role":"overview","path":"docs/index.md","digest":"sha256:doc1"},{"docKey":"runbook","title":"Runbook","role":"runbook","path":"docs/runbook.md","digest":"sha256:doc2"}]}`, ""),
		},
		doc: []byte("The service overview body."),
	}
	p := &Provider{API: api}
	uri := "catalog://ws_1/" + EncodeEntityKey("component:default/api")
	text, err := readResource(t, p, uri)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	wantCalls := []string{
		"ListCatalogEntities org=ws_1 kind= q=component:default/api cursor= limit=100",
		"ListCatalogDocs org=ws_1 entityRef=component:default/api role= limit=100",
		"ReadCatalogDoc org=ws_1 digest=sha256:doc1",
	}
	for i, want := range wantCalls {
		if api.calls[i] != want {
			t.Errorf("call %d = %q, want %q", i, api.calls[i], want)
		}
	}
	for _, marker := range []string{
		"# component:default/api — api",
		"- **Kind:** Component",
		"- **Owner:** team-a",
		"- **Lifecycle:** production",
		"- **Description:** the API",
		"- **System:** core",
		"## Relations",
		"- dependsOn → `component:default/db`",
		"## Provenance",
		"- Project `prj_1` · environment project-wide · commit abc123",
		"- Snapshot `sha256:head`",
		"## Docs",
		"| overview | Overview | overview | `docs/index.md` | `sha256:doc1` |",
		"| runbook | Runbook | runbook | `docs/runbook.md` | `sha256:doc2` |",
		"## Overview",
		"The service overview body.",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("markdown lacks %q", marker)
		}
	}
	if strings.Contains(text, "api-gateway") {
		t.Errorf("exact-ref filtering failed: near-miss rendered")
	}
}

// TestCatalogEntityResourceFallbacks: no docs, no overview, null owner —
// the TS renderings' dash/None/No-docs fallbacks.
func TestCatalogEntityResourceFallbacks(t *testing.T) {
	api := &fakeAPI{pages: []*remotestate.PlatformPage{
		page(`{"entities":[{"entityRef":"component:default/api","name":"api","kind":"Component","relations":[],"sourceProjectId":"prj_1","headDigest":"sha256:head"}]}`, ""),
		page(`{"docs":[]}`, ""),
	}}
	text, err := readResource(t, &Provider{API: api}, "catalog://ws_1/"+EncodeEntityKey("component:default/api"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	for _, marker := range []string{"- **Owner:** —", "None declared.", "No catalog docs."} {
		if !strings.Contains(text, marker) {
			t.Errorf("markdown lacks fallback %q", marker)
		}
	}
	if strings.Contains(text, "## Overview") {
		t.Errorf("no overview doc but an Overview section rendered")
	}
	if len(api.calls) != 2 {
		t.Errorf("no ReadCatalogDoc without an overview row: %v", api.calls)
	}
}

// TestRunSummaryResourceRead: run + plan-DAG jobs table, decoding the wire's
// {run:{…}} / {jobs:[…]} envelopes.
func TestRunSummaryResourceRead(t *testing.T) {
	api := &fakeAPI{pages: []*remotestate.PlatformPage{
		page(`{"run":{"runId":"01RUN","projectId":"prj_1","environment":"prod","status":"failed","source":"ci","git":{"ref":"main","commit":"abc123","dirty":true},"createdBy":{"id":"usr_1","displayName":"Rahul"},"createdAt":"2026-07-01T00:00:00Z","startedAt":"2026-07-01T00:01:00Z","finishedAt":null,"jobCounts":{"queued":1,"running":0,"succeeded":2,"failed":1}}}`, ""),
		page(`{"jobs":[{"jobId":"build","status":"succeeded","attempt":1,"component":"component:default/api","errorText":null},{"jobId":"deploy","status":"failed","attempt":2,"component":null,"errorText":"boom"}]}`, ""),
	}}
	p := &Provider{API: api}
	text, err := readResource(t, p, "runs://ws_1/prj_1/01RUN")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	wantCalls := []string{
		"GetPlatformRun org=ws_1 project=prj_1 run=01RUN",
		"ListPlatformRunJobs org=ws_1 project=prj_1 run=01RUN",
	}
	for i, want := range wantCalls {
		if api.calls[i] != want {
			t.Errorf("call %d = %q, want %q", i, api.calls[i], want)
		}
	}
	for _, marker := range []string{
		"# Run 01RUN — failed",
		"- **Project:** `prj_1` · **Environment:** prod",
		"- **Source:** ci · `main` @ `abc123` (dirty)",
		"- **Created:** 2026-07-01T00:00:00Z by Rahul",
		"- **Started:** 2026-07-01T00:01:00Z · **Finished:** —",
		"- **Job counts:** 1 queued · 0 running · 2 succeeded · 1 failed",
		"## Jobs",
		"| Job | Status | Attempt | Component | Error |",
		"| build | succeeded | 1 | component:default/api | — |",
		"| deploy | failed | 2 | — | boom |",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("markdown lacks %q", marker)
		}
	}
}

// TestResourceReadErrors: platform errors keep their code in the returned
// error's message (errText semantics — the loop turns it into a protocol
// error), not-found and malformed URIs are coded the same way, and an
// unknown scheme is simply not owned.
func TestResourceReadErrors(t *testing.T) {
	apiErr := &remotestate.APIError{Code: "forbidden", Message: "missing member role", RequestID: "req_42", Status: 403}
	p := &Provider{API: &fakeAPI{err: apiErr}}
	if _, err := readResource(t, p, "catalog://ws_1/"+EncodeEntityKey("component:default/api")); err == nil ||
		err.Error() != "forbidden: missing member role (requestId: req_42)" {
		t.Fatalf("catalog read error = %v", err)
	}
	if _, err := readResource(t, p, "runs://ws_1/prj_1/01RUN"); err == nil || !strings.HasPrefix(err.Error(), "forbidden:") {
		t.Fatalf("run read error = %v", err)
	}

	// Entity absent from the list page: the TS not_found framing.
	p = &Provider{API: &fakeAPI{page: page(`{"entities":[]}`, "")}}
	if _, err := readResource(t, p, "catalog://ws_1/"+EncodeEntityKey("component:default/nope")); err == nil ||
		!strings.Contains(err.Error(), `not_found: no catalog entity with ref "component:default/nope" in workspace ws_1`) {
		t.Fatalf("not-found error = %v", err)
	}

	// Malformed URIs: wrong segment count and a non-base64url key.
	for uri, frag := range map[string]string{
		"catalog://ws-only":       "catalog://{workspace}/{entityKey}",
		"runs://ws_1/prj_1":       "runs://{workspace}/{project}/{runId}",
		"catalog://ws_1/not!b64":  "must be base64url",
		"catalog://ws_1/abcde":    "not valid base64url", // charset-clean but undecodable (len%4 == 1)
		"catalog://ws_1/n!/extra": "catalog://{workspace}/{entityKey}",
	} {
		if _, err := readResource(t, &Provider{API: &fakeAPI{}}, uri); err == nil ||
			!strings.Contains(err.Error(), "validation_failed") || !strings.Contains(err.Error(), frag) {
			t.Errorf("%s: err = %v, want validation_failed mentioning %q", uri, err, frag)
		}
	}

	// A scheme outside both templates is not owned.
	if _, owned, _ := (&Provider{API: &fakeAPI{}}).ReadResource(context.Background(), "other://ws_1/x"); owned {
		t.Fatal("unknown scheme must not be owned")
	}
}

// TestResourceOverviewByteCap: an oversized overview body carries the exact
// truncation marker inside the Overview section.
func TestResourceOverviewByteCap(t *testing.T) {
	api := &fakeAPI{
		pages: []*remotestate.PlatformPage{
			page(`{"entities":[{"entityRef":"component:default/api","name":"api","kind":"Component","sourceProjectId":"prj_1","headDigest":"sha256:head"}]}`, ""),
			page(`{"docs":[{"docKey":"overview","title":"Overview","role":"overview","path":"docs/index.md","digest":"sha256:doc1"}]}`, ""),
		},
		doc: []byte(strings.Repeat("x", maxToolBytes+100)),
	}
	text, err := readResource(t, &Provider{API: api}, "catalog://ws_1/"+EncodeEntityKey("component:default/api"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(text, "[truncated — 100 more bytes; refine your query or use fromSeq/cursor]") {
		t.Fatalf("overview body not byte-capped with the marker; tail: %q", text[len(text)-100:])
	}
}
