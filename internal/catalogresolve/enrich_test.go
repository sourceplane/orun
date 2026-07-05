package catalogresolve

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func enrichManifests() []*catalogmodel.ComponentManifest {
	return []*catalogmodel.ComponentManifest{
		{
			Identity: catalogmodel.ComponentIdentity{ComponentKey: "acme/repo/api", Name: "api"},
			Metadata: catalogmodel.ComponentMetadata{Owner: "group:platform"},
			Spec: catalogmodel.ComponentSpec{
				System: "core", Domain: "identity",
				Environments: map[string]catalogmodel.ComponentEnvironment{"prod": {}},
			},
		},
	}
}

func TestResolveEnrichments(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docs/domains/identity.md", "# Identity domain\n")
	intent := &intentFile{Catalog: &intentCatalogBlock{Entities: map[string]*intentEntityEnrichment{
		"domain/identity": {
			Description: "Sign-in and sessions.",
			Owner:       "group:platform",
			Tags:        []string{"auth"},
			Docs:        &intentRepoDocs{Overview: "docs/domains/identity.md"},
		},
		"system/core":      {Description: "The core system."},
		"environment/prod": {Description: "Production."},
		"group/platform":   {Description: "The platform group."},
	}}}

	enr, issues := resolveEnrichments(intent, enrichManifests(), newDocResolveContext(dir))
	for _, i := range issues {
		t.Errorf("unexpected issue: %s %s", i.Code, i.Message)
	}
	if len(enr) != 4 {
		t.Fatalf("enrichments = %d, want 4", len(enr))
	}
	// Sorted by key: domain/identity first.
	d := enr[0]
	if d.Kind != "Domain" || d.Name != "identity" || d.Description != "Sign-in and sessions." {
		t.Errorf("domain enrichment = %+v", d)
	}
	if len(d.Docs) != 1 || !d.Docs[0].Attached() || d.Docs[0].Key != "overview" {
		t.Errorf("domain docs = %+v", d.Docs)
	}
}

func TestResolveEnrichmentsValidation(t *testing.T) {
	intent := &intentFile{Catalog: &intentCatalogBlock{Entities: map[string]*intentEntityEnrichment{
		"component/api":  {Description: "nope"},
		"widget/thing":   {Description: "nope"},
		"badkey":         {Description: "nope"},
		"domain/unknown": {Description: "orphan"},
	}}}
	enr, issues := resolveEnrichments(intent, enrichManifests(), nil)

	codes := map[string]Severity{}
	for _, i := range issues {
		codes[i.Code] = i.Severity
	}
	if sev, ok := codes["catalog.entities.kind.declared"]; !ok || sev != SeverityError {
		t.Errorf("declared-kind enrichment must error: %v", codes)
	}
	if sev, ok := codes["catalog.entities.kind.unknown"]; !ok || sev != SeverityError {
		t.Errorf("unknown-kind enrichment must error: %v", codes)
	}
	if sev, ok := codes["catalog.entities.key.invalid"]; !ok || sev != SeverityError {
		t.Errorf("malformed key must error: %v", codes)
	}
	if sev, ok := codes["catalog.entities.target.unreferenced"]; !ok || sev != SeverityWarning {
		t.Errorf("orphan target must warn (never error, never create): %v", codes)
	}

	// Enrich, never create: the orphan still yields an enrichment value (the
	// assembly ignores it when nothing matches), the invalid ones do not.
	if len(enr) != 1 || enr[0].Kind != "Domain" || enr[0].Name != "unknown" {
		t.Errorf("enr = %+v", enr)
	}
}

func TestResolveEnrichmentsNilIntent(t *testing.T) {
	if enr, issues := resolveEnrichments(nil, nil, nil); enr != nil || issues != nil {
		t.Errorf("nil intent should be a no-op, got %v / %v", enr, issues)
	}
}

func TestEnrichmentIssueMessagesNameTheKey(t *testing.T) {
	intent := &intentFile{Catalog: &intentCatalogBlock{Entities: map[string]*intentEntityEnrichment{
		"repo/self": {},
	}}}
	_, issues := resolveEnrichments(intent, nil, nil)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, `"repo/self"`) {
		t.Errorf("issues = %+v", issues)
	}
}
