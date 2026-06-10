package nodes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
)

func mem() *objectstore.MemStore { return objectstore.NewMemStore("") }

// findEntry returns the id of the named entry in a tree.
func findEntry(t *testing.T, s *objectstore.MemStore, tree objectstore.ObjectID, name string) (objectstore.ObjectID, objectstore.Kind) {
	t.Helper()
	entries, err := s.GetTree(context.Background(), tree)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e.ID, e.Kind
		}
	}
	t.Fatalf("entry %q not found in tree %s", name, tree)
	return "", ""
}

func blobBody(t *testing.T, s *objectstore.MemStore, id objectstore.ObjectID) string {
	t.Helper()
	_, body, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return string(body)
}

func TestAssembleSourceAndTrigger(t *testing.T) {
	t.Parallel()
	s := mem()
	ctx := context.Background()
	srcID, err := AssembleSource(ctx, s, SourceSnapshot{Scope: ScopeMain, Repo: "ns/repo", HeadRevision: "abc123"})
	if err != nil {
		t.Fatalf("AssembleSource: %v", err)
	}
	body := blobBody(t, s, srcID)
	if !strings.Contains(body, `"kind":"SourceSnapshot"`) {
		t.Fatalf("source body = %s", body)
	}
	trgID, err := AssembleTrigger(ctx, s, TriggerOccurrence{
		TriggerID: "trg_1", TriggerName: "system.manual", RevisionID: goodID("c"),
		CreatedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AssembleTrigger: %v", err)
	}
	if !strings.Contains(blobBody(t, s, trgID), `"triggerId":"trg_1"`) {
		t.Fatalf("trigger body missing id")
	}
}

func TestAssembleRevisionTreeAndNoSelfID(t *testing.T) {
	t.Parallel()
	s := mem()
	ctx := context.Background()
	rev := PlanRevision{CatalogID: goodID("a"), Scope: RevisionScope{Mode: "full"}, JobCount: 2}
	revID, err := AssembleRevision(ctx, s, rev, []byte(`{"plan":"A"}`))
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	// The revision tree has revision.json + plan.json.
	revBlob, _ := findEntry(t, s, revID, fileRevision)
	planBlob, _ := findEntry(t, s, revID, filePlan)
	revBody := blobBody(t, s, revBlob)
	// No-self-id + no-trigger + no-timestamp in revision.json.
	for _, banned := range []string{string(revID), "trigger", "createdAt", "resolvedAt", "executionId"} {
		if strings.Contains(revBody, banned) {
			t.Fatalf("revision.json contains banned token %q: %s", banned, revBody)
		}
	}
	// planHash equals the plan blob id.
	if !strings.Contains(revBody, `"planHash":"`+string(planBlob)+`"`) {
		t.Fatalf("planHash != plan blob id: %s", revBody)
	}
}

func TestAssembleRevisionDedupAcrossTriggers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	rev := PlanRevision{CatalogID: goodID("a"), Scope: RevisionScope{Mode: "full"}}
	plan := []byte(`{"plan":"same"}`)
	r1, err := AssembleRevision(ctx, s, rev, plan)
	if err != nil {
		t.Fatalf("r1: %v", err)
	}
	r2, err := AssembleRevision(ctx, s, rev, plan)
	if err != nil {
		t.Fatalf("r2: %v", err)
	}
	if r1 != r2 {
		t.Fatalf("identical plan produced different revision ids: %s vs %s", r1, r2)
	}
	// Two distinct triggers reference the one shared revision.
	t1, _ := AssembleTrigger(ctx, s, TriggerOccurrence{TriggerID: "trg_1", TriggerName: "n", RevisionID: string(r1), CreatedAt: time.Now()})
	t2, _ := AssembleTrigger(ctx, s, TriggerOccurrence{TriggerID: "trg_2", TriggerName: "n", RevisionID: string(r1), CreatedAt: time.Now()})
	if t1 == t2 {
		t.Fatalf("distinct triggers produced the same object id")
	}
}

func TestAssembleRevisionIdentityDeterministicAcrossStores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rev := PlanRevision{CatalogID: goodID("a"), Scope: RevisionScope{Mode: "full"}, JobCount: 3}
	plan := []byte(`{"plan":"det"}`)
	a, err := AssembleRevision(ctx, mem(), rev, plan)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := AssembleRevision(ctx, mem(), rev, plan)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a != b {
		t.Fatalf("revision id not deterministic across stores: %s vs %s", a, b)
	}
}

func TestAssembleCatalogTreeAndNoSelfID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/api-edge", Name: "api-edge", Namespace: "ns", Repo: "repo"}, Type: "cloudflare-worker"},
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/worker", Name: "worker", Namespace: "ns", Repo: "repo"}},
	}
	graphs := []CatalogGraph{{EdgeKind: "dependencies", Nodes: []GraphNode{{Key: "ns/repo/api-edge", Kind: "Component", Name: "api-edge"}}}}
	catID, err := AssembleCatalog(ctx, s, CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 1}, manifests, graphs, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}
	// Tree has catalog.json + components/ + graph/.
	catBlob, _ := findEntry(t, s, catID, fileCatalog)
	_, compKind := findEntry(t, s, catID, dirComponents)
	_, graphKind := findEntry(t, s, catID, dirGraph)
	if compKind != objectstore.KindTree || graphKind != objectstore.KindTree {
		t.Fatalf("components/graph not trees: %s %s", compKind, graphKind)
	}
	catBody := blobBody(t, s, catBlob)
	if strings.Contains(catBody, string(catID)) {
		t.Fatalf("catalog.json embeds its own id: %s", catBody)
	}
	if !strings.Contains(catBody, `"componentCount":2`) {
		t.Fatalf("componentCount wrong: %s", catBody)
	}
	// Components subtree has two manifest blobs.
	compTree, _ := findEntry(t, s, catID, dirComponents)
	entries, _ := s.GetTree(ctx, compTree)
	if len(entries) != 2 {
		t.Fatalf("components subtree = %d entries, want 2", len(entries))
	}
}

func TestAssembleCatalogWritesRelations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo"},
			Relations: []EntityRelation{
				{Type: "dependsOn", To: "ns/repo/db", ToKind: "Component", Include: "always"},
				{Type: "ownedBy", To: "team-x", ToKind: "Group"},
			}},
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/db", Name: "db", Namespace: "ns", Repo: "repo"}},
	}
	catID, err := AssembleCatalog(ctx, s, CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 3}, manifests, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}
	relBlob, relKind := findEntry(t, s, catID, fileRelations)
	if relKind != objectstore.KindBlob {
		t.Fatalf("relations.json not a blob: %s", relKind)
	}
	rg, err := Decode[RelationGraph]([]byte(blobBody(t, s, relBlob)))
	if err != nil {
		t.Fatalf("decode relations: %v", err)
	}
	if rg.Kind != KindRelationGraph || len(rg.Edges) != 2 {
		t.Fatalf("relation graph = %+v", rg)
	}
	// Edges are sorted by (from, fromKind, type, to): dependsOn before ownedBy.
	if rg.Edges[0].Type != "dependsOn" || rg.Edges[0].From != "ns/repo/api" || rg.Edges[0].FromKind != "Component" {
		t.Fatalf("first edge = %+v", rg.Edges[0])
	}
	if rg.Edges[0].Include != "always" || rg.Edges[1].Type != "ownedBy" {
		t.Fatalf("edges = %+v", rg.Edges)
	}
}

func TestRelationGraphValidate(t *testing.T) {
	t.Parallel()
	if err := (RelationGraph{Kind: "Nope"}).Validate(); err == nil {
		t.Fatal("bad kind accepted")
	}
	bad := RelationGraph{Kind: KindRelationGraph, Edges: []RelationEdge{{From: "a", Type: "", To: "b"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("empty edge type accepted")
	}
	ok := RelationGraph{Kind: KindRelationGraph, Edges: []RelationEdge{{From: "a", Type: "dependsOn", To: "b"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
}

func TestAssembleCatalogWritesImpact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", Path: "apps/api/component.yaml"}},
	}
	own := ImpactOwnership{
		Components:          map[string]string{"apps/api": "ns/repo/api"},
		GlobalPaths:         []string{"intent.yaml"},
		StructuralFilenames: []string{"component.yaml"},
		IgnoreDirs:          []string{".git"},
	}
	fps := []ComponentFingerprint{
		{ComponentKey: "ns/repo/api", Dir: "apps/api", Subtree: "sha256:fp", GlobalDigest: "sha256:g"},
	}
	catID, err := AssembleCatalog(ctx, s, CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 1}, manifests, nil, own, fps)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}
	// impact/ is present and a tree.
	impactTree, impactKind := findEntry(t, s, catID, dirImpact)
	if impactKind != objectstore.KindTree {
		t.Fatalf("impact/ not a tree: %s", impactKind)
	}
	// impact/ownership.json decodes with the defaulted Kind/SchemaVersion.
	ownBlob, _ := findEntry(t, s, impactTree, fileOwnership)
	decoded, err := Decode[ImpactOwnership]([]byte(blobBody(t, s, ownBlob)))
	if err != nil {
		t.Fatalf("decode ownership: %v", err)
	}
	if decoded.Kind != KindImpactOwnership || decoded.SchemaVersion != 1 {
		t.Fatalf("ownership defaults not applied: %+v", decoded)
	}
	if decoded.Components["apps/api"] != "ns/repo/api" {
		t.Fatalf("ownership components = %v", decoded.Components)
	}
	// impact/fingerprints/<name>.json decodes with the defaulted Kind/SchemaVersion.
	fpTree, fpKind := findEntry(t, s, impactTree, dirFingerprints)
	if fpKind != objectstore.KindTree {
		t.Fatalf("fingerprints/ not a tree: %s", fpKind)
	}
	fpBlob, _ := findEntry(t, s, fpTree, "api.json")
	fp, err := Decode[ComponentFingerprint]([]byte(blobBody(t, s, fpBlob)))
	if err != nil {
		t.Fatalf("decode fingerprint: %v", err)
	}
	if fp.Kind != KindComponentFingerprint || fp.SchemaVersion != 1 || fp.Subtree != "sha256:fp" {
		t.Fatalf("fingerprint defaults/content wrong: %+v", fp)
	}
}

func TestAssembleCatalogRejectsBadFingerprint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bad := []ComponentFingerprint{{ComponentKey: "ns/repo/api", Dir: "apps/api"}} // empty subtree
	if _, err := AssembleCatalog(ctx, mem(), CatalogSnapshot{SourceID: goodID("a")}, nil, nil, ImpactOwnership{}, bad); err == nil {
		t.Fatalf("AssembleCatalog accepted invalid fingerprint")
	}
}

func TestAssembleCatalogRejectsBadOwnership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bad := ImpactOwnership{Components: map[string]string{"bad/dir/": "ns/repo/api"}}
	if _, err := AssembleCatalog(ctx, mem(), CatalogSnapshot{SourceID: goodID("a")}, nil, nil, bad, nil); err == nil {
		t.Fatalf("AssembleCatalog accepted invalid ownership dir")
	}
}

func TestAssembleCatalogOrderIndependent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/a", Name: "a"}},
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/b", Name: "b"}},
	}
	rev := []ComponentManifest{m[1], m[0]}
	cat := CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 1}
	id1, err := AssembleCatalog(ctx, mem(), cat, m, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("id1: %v", err)
	}
	id2, err := AssembleCatalog(ctx, mem(), cat, rev, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("id2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("catalog id depends on manifest order: %s vs %s", id1, id2)
	}
}

func TestAssembleExecutionTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	in := ExecutionInput{
		Execution: ExecutionRun{
			ExecutionID: "exec_1", ExecutionKey: "run-001", RevisionID: goodID("d"),
			TriggerID: "trg_1", Status: StatusSucceeded, StartedAt: now,
		},
		Jobs: []JobInput{{
			Record: JobRun{JobID: "api-edge@deploy", Folder: "j-a8f3", Status: StatusSucceeded},
			Attempts: []AttemptInput{{
				Record: JobAttempt{Attempt: 1, Status: StatusSucceeded},
				Steps: []StepInput{{
					Record: StepAttempt{StepID: "build", Status: StatusSucceeded, ExitCode: 0},
					Log:    []byte("build log output"),
				}},
			}},
		}},
		Events:    []NamedBlob{{Name: "00000000000000000001-execution-created.json", Data: []byte(`{"kind":"ExecutionEvent"}`)}},
		Artifacts: []NamedBlob{{Name: "out.txt", Data: []byte("artifact")}},
	}
	execID, err := AssembleExecution(ctx, s, in)
	if err != nil {
		t.Fatalf("AssembleExecution: %v", err)
	}
	// Top tree shape.
	for _, name := range []string{fileExecution, dirJobs, dirEvents, dirArtifacts} {
		findEntry(t, s, execID, name)
	}
	// Drill jobs/j-a8f3/attempts/1/steps/s-build.json and confirm the log dedups.
	jobsTree, _ := findEntry(t, s, execID, dirJobs)
	jobTree, _ := findEntry(t, s, jobsTree, "j-a8f3")
	attemptsTree, _ := findEntry(t, s, jobTree, dirAttempts)
	attemptTree, _ := findEntry(t, s, attemptsTree, "1")
	stepsTree, _ := findEntry(t, s, attemptTree, dirSteps)
	stepBlob, _ := findEntry(t, s, stepsTree, "s-build.json")
	stepBody := blobBody(t, s, stepBlob)
	if !strings.Contains(stepBody, `"stepId":"build"`) || !strings.Contains(stepBody, `"logId":"sha256:`) {
		t.Fatalf("step body = %s", stepBody)
	}
	// execution.json carries jobIds mapping.
	execBlob, _ := findEntry(t, s, execID, fileExecution)
	if !strings.Contains(blobBody(t, s, execBlob), `"j-a8f3"`) {
		t.Fatalf("execution.json missing jobIds")
	}
}

func TestAssembleExecutionEmptyJobsIsValid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	id, err := AssembleExecution(ctx, s, ExecutionInput{
		Execution: ExecutionRun{ExecutionID: "exec_x", RevisionID: goodID("d"), Status: StatusSucceeded, StartedAt: time.Now()},
	})
	if err != nil {
		t.Fatalf("AssembleExecution(empty): %v", err)
	}
	// jobs/events/artifacts subtrees still present (empty).
	for _, name := range []string{dirJobs, dirEvents, dirArtifacts} {
		findEntry(t, s, id, name)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	rev := PlanRevision{Kind: KindPlanRevision, PlanHash: goodID("b"), Scope: RevisionScope{Mode: "full"}, JobCount: 4}
	b, err := Encode(rev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode[PlanRevision](b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.PlanHash != rev.PlanHash || got.JobCount != 4 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if _, err := Decode[PlanRevision]([]byte("{not json")); err == nil {
		t.Fatalf("Decode(garbage) succeeded")
	}
}

func TestAssembleValidationErrorsPropagate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mem()
	if _, err := AssembleSource(ctx, s, SourceSnapshot{Scope: "weird"}); err == nil {
		t.Fatalf("AssembleSource accepted bad scope")
	}
	if _, err := AssembleCatalog(ctx, s, CatalogSnapshot{SourceID: goodID("a")},
		[]ComponentManifest{{Identity: ComponentIdentity{ComponentKey: "bad", Name: "bad"}}}, nil, ImpactOwnership{}, nil); err == nil {
		t.Fatalf("AssembleCatalog accepted bad manifest")
	}
}
