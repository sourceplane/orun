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

func TestEnvironmentEnrichmentsDeclareDoors(t *testing.T) {
	intent := &intentFile{
		Environments: map[string]*intentEnvironment{
			"dev":   {URL: "https://dev.kartha.example"},
			"stage": {URL: "https://stage.kartha.example", Links: []intentRepoLink{{Title: "API", URL: "https://api-stage.kartha.example"}}},
			"prod": {
				Description: "Where customers are.",
				URL:         "https://app.kartha.example",
				Promotion:   &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "stage"}}},
			},
			"preview": {}, // declared, nothing else: still materializes
		},
		Catalog: &intentCatalogBlock{Entities: map[string]*intentEntityEnrichment{
			// Enrichment for a declared environment: fills what the block
			// left empty, links follow the declared ones, no warning.
			"environment/stage": {Description: "Pre-production.", Links: []intentRepoLink{{Title: "Status", URL: "https://status.kartha.example"}}},
		}},
	}
	enr, issues := resolveEnrichments(intent, enrichManifests(), nil)
	for _, i := range issues {
		t.Errorf("unexpected issue: %s %s", i.Code, i.Message)
	}
	byName := map[string]EntityEnrichment{}
	for _, e := range enr {
		if e.Kind != catalogmodel.EntityKindEnvironment {
			t.Errorf("unexpected kind %s", e.Kind)
		}
		byName[e.Name] = e
	}
	if len(byName) != 4 {
		t.Fatalf("environments = %v, want 4", byName)
	}
	prod := byName["prod"]
	if !prod.Declared || prod.URL != "https://app.kartha.example" || prod.Description != "Where customers are." {
		t.Errorf("prod = %+v", prod)
	}
	if len(prod.Links) != 1 || prod.Links[0].Role != catalogmodel.LinkRolePrimary || prod.Links[0].Title != "prod" {
		t.Errorf("prod links = %+v (links[0] must be the front door, role primary)", prod.Links)
	}
	if prod.Order != 1 || byName["stage"].Order != 0 || byName["dev"].Order != 0 {
		t.Errorf("orders = prod:%d stage:%d dev:%d, want 1/0/0", prod.Order, byName["stage"].Order, byName["dev"].Order)
	}
	stage := byName["stage"]
	if stage.Description != "Pre-production." {
		t.Errorf("stage description not filled from enrichment: %+v", stage)
	}
	wantLinks := []string{"https://stage.kartha.example", "https://api-stage.kartha.example", "https://status.kartha.example"}
	if len(stage.Links) != len(wantLinks) {
		t.Fatalf("stage links = %+v", stage.Links)
	}
	for i, u := range wantLinks {
		if stage.Links[i].URL != u {
			t.Errorf("stage links[%d] = %s, want %s", i, stage.Links[i].URL, u)
		}
	}
	if stage.Links[0].Role != catalogmodel.LinkRolePrimary || stage.Links[1].Role != "" {
		t.Errorf("stage link roles = %+v", stage.Links)
	}
	if p := byName["preview"]; !p.Declared || p.URL != "" || len(p.Links) != 0 {
		t.Errorf("preview = %+v", p)
	}
}

func TestEnvironmentEnrichmentsRejectRelativeURLs(t *testing.T) {
	intent := &intentFile{Environments: map[string]*intentEnvironment{
		"dev":  {URL: "dev.kartha.example"},
		"prod": {URL: "https://app.kartha.example", Links: []intentRepoLink{{Title: "API", URL: "/api"}}},
	}}
	enr, issues := resolveEnrichments(intent, nil, nil)
	codes := map[string]int{}
	for _, i := range issues {
		if i.Severity != SeverityError {
			t.Errorf("issue %s should be an error", i.Code)
		}
		codes[i.Code]++
	}
	if codes["environments.url.invalid"] != 1 || codes["environments.links.url.invalid"] != 1 {
		t.Errorf("issue codes = %v", codes)
	}
	for _, e := range enr {
		if e.Name == "dev" && (e.URL != "" || len(e.Links) != 0) {
			t.Errorf("a rejected url must not ride: %+v", e)
		}
		if e.Name == "prod" && len(e.Links) != 1 {
			t.Errorf("prod keeps its front door, drops the bad link: %+v", e.Links)
		}
	}
}

func TestPromotionOrdersFollowTheChain(t *testing.T) {
	envs := map[string]*intentEnvironment{
		"dev":    {},
		"stage":  {Promotion: &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "dev"}}}},
		"prod":   {Promotion: &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "stage"}, {Environment: "dev"}}}},
		"loopA":  {Promotion: &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "loopB"}}}},
		"loopB":  {Promotion: &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "loopA"}}}},
		"orphan": {Promotion: &intentPromotion{DependsOn: []intentPromotionDependency{{Environment: "nowhere"}}}},
	}
	o := promotionOrders(envs)
	want := map[string]int{"dev": 0, "stage": 1, "prod": 2, "orphan": 0}
	for n, w := range want {
		if o[n] != w {
			t.Errorf("order[%s] = %d, want %d", n, o[n], w)
		}
	}
	// A cycle terminates; both members get a finite order.
	if o["loopA"] < 0 || o["loopB"] < 0 {
		t.Errorf("cycle orders = %d/%d", o["loopA"], o["loopB"])
	}
}
