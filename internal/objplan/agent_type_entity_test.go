package objplan

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/agenttype"
	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

func agentView() *catalogresolve.CatalogView {
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Namespace: "sourceplane",
			Repo:      "orun",
			Manifests: []*catalogmodel.ComponentManifest{
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "sourceplane/orun/billing-worker", Name: "billing-worker", Namespace: "sourceplane", Repo: "orun"}},
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "sourceplane/orun/api-edge", Name: "api-edge", Namespace: "sourceplane", Repo: "orun"}},
			},
			AgentTypes: []*agenttype.Decl{{
				Name:            "implementer",
				Harness:         "claude-code",
				Model:           "claude-opus-4-8",
				Runtime:         &nodes.AgentRuntime{Effort: "high", MaxTokens: 64000},
				AutonomyDefault: "assist",
				Tools: nodes.AgentToolPolicy{
					Allow: []string{"work_get"},
					Ask:   []string{"contract_propose"},
					Deny:  []string{"*"},
				},
				MayAffect: []string{"billing-*"},
				Owner:     "sourceplane/team/payments",
				Extends:   "base-orun-literacy",
				Body:      []byte("# Implementer\n\npersona\n"),
				Path:      "agents/implementer.md",
			}},
		},
	}
}

func TestBuildCatalogNodesEmitsAgentTypeEntity(t *testing.T) {
	cat, _, _, _, _ := BuildCatalogNodes(agentView(), 15, nil, nil)
	if len(cat.DeclaredEntities) != 1 {
		t.Fatalf("DeclaredEntities = %d, want 1", len(cat.DeclaredEntities))
	}
	e := cat.DeclaredEntities[0]
	if e.Kind != nodes.EntityKindAgentType || e.Identity.EntityKey != "sourceplane/orun/implementer" {
		t.Fatalf("identity = %+v (kind %q)", e.Identity, e.Kind)
	}
	if e.Metadata["harness"] != "claude-code" || e.Metadata["model"] != "claude-opus-4-8" ||
		e.Metadata["autonomyDefault"] != "assist" || e.Metadata["extends"] != "base-orun-literacy" {
		t.Errorf("metadata = %v", e.Metadata)
	}
	if e.Ownership["owner"] != "sourceplane/team/payments" {
		t.Errorf("ownership = %v", e.Ownership)
	}
	spec := e.Spec
	if spec == nil {
		t.Fatal("spec block missing")
	}
	if ma, _ := spec["mayAffect"].([]any); len(ma) != 1 || ma[0] != "billing-*" {
		t.Errorf("spec.mayAffect = %v", spec["mayAffect"])
	}
	// The glob resolves to a typed edge against billing-worker only.
	if len(e.Relations) != 1 || e.Relations[0].Type != "mayAffect" ||
		e.Relations[0].To != "sourceplane/orun/billing-worker" || e.Relations[0].ToKind != nodes.EntityKindComponent {
		t.Errorf("relations = %+v", e.Relations)
	}
	// The persona rides as a pending doc under key "persona".
	if string(e.PendingDocs["persona"]) == "" {
		t.Error("persona pending doc missing")
	}
}

func TestAgentTypeEntityAssemblesIntoCatalogTree(t *testing.T) {
	ctx := context.Background()
	mem := objectstore.NewMemStore(objectstore.AlgoSHA256)
	cat, manifests, graphs, ownership, fps := BuildCatalogNodes(agentView(), 15, nil, nil)
	srcID, err := mem.PutBlob(ctx, []byte("src"))
	if err != nil {
		t.Fatal(err)
	}
	cat.SourceID = string(srcID)
	id, err := nodes.AssembleCatalog(ctx, mem, cat, manifests, graphs, ownership, fps)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	entries, err := mem.GetTree(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var entitiesID objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "entities" {
			entitiesID = e.ID
		}
	}
	if entitiesID == "" {
		t.Fatalf("no entities/ subtree in %v", entries)
	}
	kinds, err := mem.GetTree(ctx, entitiesID)
	if err != nil {
		t.Fatal(err)
	}
	var agentKindID objectstore.ObjectID
	for _, e := range kinds {
		if e.Name == "AgentType" {
			agentKindID = e.ID
		}
	}
	if agentKindID == "" {
		t.Fatalf("no entities/AgentType/ subtree in %v", kinds)
	}
	files, err := mem.GetTree(ctx, agentKindID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "implementer.json" {
		t.Fatalf("entities/AgentType = %v", files)
	}
	// The projected entity's persona doc digest resolves to the verbatim body.
	_, b, err := mem.Get(ctx, files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	ent, err := nodes.Decode[nodes.Entity](b)
	if err != nil {
		t.Fatal(err)
	}
	persona, _ := ent.Docs["persona"].(map[string]any)
	digest, _ := persona["digest"].(string)
	if digest == "" {
		t.Fatalf("persona digest not stamped: %v", ent.Docs)
	}
	_, body, err := mem.Get(ctx, objectstore.ObjectID(digest))
	if err != nil || string(body) != "# Implementer\n\npersona\n" {
		t.Fatalf("persona blob mismatch: %q err=%v", body, err)
	}
}
