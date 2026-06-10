package nodes

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/objectstore"
)

// ObjectID is re-exported for callers that hold node ids without importing the
// object store directly.
type ObjectID = objectstore.ObjectID

// store is the subset of objectstore.ObjectStore the assemblers need.
type store interface {
	PutBlob(ctx context.Context, data []byte) (objectstore.ObjectID, error)
	PutTree(ctx context.Context, entries []objectstore.TreeEntry) (objectstore.ObjectID, error)
}

func blobEntry(name string, id objectstore.ObjectID) objectstore.TreeEntry {
	return objectstore.TreeEntry{Name: name, Kind: objectstore.KindBlob, ID: id}
}

func treeEntry(name string, id objectstore.ObjectID) objectstore.TreeEntry {
	return objectstore.TreeEntry{Name: name, Kind: objectstore.KindTree, ID: id}
}

// sanitizeSegment folds an arbitrary string into the tree-name alphabet so it
// can be a folder/file name; the original value is always preserved inside the
// record JSON.
func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "x"
	}
	return out
}

// AssembleSource writes the source record as a blob and returns its id. The id
// is input-addressed because the record is a pure function of git state.
func AssembleSource(ctx context.Context, s store, src SourceSnapshot) (ObjectID, error) {
	src.Kind = KindSourceSnapshot
	if err := src.Validate(); err != nil {
		return "", err
	}
	b, err := Encode(src)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, b)
}

// AssembleTrigger writes the trigger event as a blob and returns its id.
func AssembleTrigger(ctx context.Context, s store, trg TriggerOccurrence) (ObjectID, error) {
	trg.Kind = KindTriggerOccurrence
	if err := trg.Validate(); err != nil {
		return "", err
	}
	b, err := Encode(trg)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, b)
}

// AssembleRevision writes plan.json + revision.json and the revision tree,
// returning the revision id (Merkle root). It sets PlanHash to the plan blob
// id so the revision record is a deterministic function of (plan, catalogId,
// scope) — the basis for revision dedup across triggers.
func AssembleRevision(ctx context.Context, s store, rev PlanRevision, planBytes []byte) (ObjectID, error) {
	rev.Kind = KindPlanRevision
	planID, err := s.PutBlob(ctx, planBytes)
	if err != nil {
		return "", err
	}
	rev.PlanHash = string(planID)
	if err := rev.Validate(); err != nil {
		return "", err
	}
	revBytes, err := Encode(rev)
	if err != nil {
		return "", err
	}
	revBlobID, err := s.PutBlob(ctx, revBytes)
	if err != nil {
		return "", err
	}
	return s.PutTree(ctx, []objectstore.TreeEntry{
		blobEntry(fileRevision, revBlobID),
		blobEntry(filePlan, planID),
	})
}

// AssembleCatalog writes the component manifests, graph slices, catalog record,
// and the change-detection ownership map, then the catalog tree (catalog.json +
// components/ + graph/ + impact/) and returns the catalog id (Merkle root).
// components/, graph/, and impact/ are always present (possibly empty) so the
// catalog tree shape is uniform and the id is deterministic. The catalog
// record's Components/GraphIDs/ComponentCount are populated here from the
// written children.
func AssembleCatalog(ctx context.Context, s store, cat CatalogSnapshot, manifests []ComponentManifest, graphs []CatalogGraph, ownership ImpactOwnership, fingerprints []ComponentFingerprint) (ObjectID, error) {
	cat.Kind = KindCatalogSnapshot

	compEntries := make([]objectstore.TreeEntry, 0, len(manifests))
	refs := make([]CatalogComponentRef, 0, len(manifests))
	for _, m := range manifests {
		m.Kind = KindComponentManifest
		if err := m.Validate(); err != nil {
			return "", err
		}
		mb, err := Encode(m)
		if err != nil {
			return "", err
		}
		mid, err := s.PutBlob(ctx, mb)
		if err != nil {
			return "", err
		}
		compEntries = append(compEntries, blobEntry(sanitizeSegment(m.Identity.Name)+".json", mid))
		refs = append(refs, CatalogComponentRef{
			ComponentKey: m.Identity.ComponentKey,
			Name:         m.Identity.Name,
			ManifestID:   string(mid),
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ComponentKey < refs[j].ComponentKey })

	graphEntries := make([]objectstore.TreeEntry, 0, len(graphs))
	graphIDs := make(map[string]string, len(graphs))
	for _, g := range graphs {
		g.Kind = KindCatalogGraph
		if err := g.Validate(); err != nil {
			return "", err
		}
		gb, err := Encode(g)
		if err != nil {
			return "", err
		}
		gid, err := s.PutBlob(ctx, gb)
		if err != nil {
			return "", err
		}
		graphEntries = append(graphEntries, blobEntry(sanitizeSegment(g.EdgeKind)+".json", gid))
		graphIDs[g.EdgeKind] = string(gid)
	}

	// Derive the non-Component entities (API/Resource/System/Domain/Group) from
	// the relations/contracts already carried on each manifest, and write them
	// under entities/<Kind>/ (orun-service-catalog SC3). Component blobs remain
	// under components/ in this milestone; the full unification to
	// entities/Component/ is the SC3 follow-up.
	derived := deriveEntities(manifests)
	entitiesTreeID, countsByKind, err := assembleEntities(ctx, s, derived)
	if err != nil {
		return "", err
	}
	countsByKind[EntityKindComponent] = len(refs)

	cat.Components = refs
	cat.ComponentCount = len(refs)
	cat.CountsByKind = countsByKind
	if len(graphIDs) > 0 {
		cat.GraphIDs = graphIDs
	}
	if err := cat.Validate(); err != nil {
		return "", err
	}
	catBytes, err := Encode(cat)
	if err != nil {
		return "", err
	}
	catBlobID, err := s.PutBlob(ctx, catBytes)
	if err != nil {
		return "", err
	}
	compTreeID, err := s.PutTree(ctx, compEntries)
	if err != nil {
		return "", err
	}
	graphTreeID, err := s.PutTree(ctx, graphEntries)
	if err != nil {
		return "", err
	}
	relationsBlobID, err := assembleRelations(ctx, s, manifests)
	if err != nil {
		return "", err
	}
	impactTreeID, err := assembleImpact(ctx, s, ownership, fingerprints)
	if err != nil {
		return "", err
	}
	return s.PutTree(ctx, []objectstore.TreeEntry{
		blobEntry(fileCatalog, catBlobID),
		treeEntry(dirComponents, compTreeID),
		treeEntry(dirEntities, entitiesTreeID),
		treeEntry(dirGraph, graphTreeID),
		blobEntry(fileRelations, relationsBlobID),
		treeEntry(dirImpact, impactTreeID),
	})
}

// Entity kind constants for the derived multi-kind entities (mirrors
// catalogmodel.EntityKind*; kept local so nodes carries no catalogmodel import).
const (
	EntityKindComponent   = "Component"
	EntityKindAPI         = "API"
	EntityKindResource    = "Resource"
	EntityKindSystem      = "System"
	EntityKindDomain      = "Domain"
	EntityKindGroup       = "Group"
	EntityKindEnvironment = "Environment"
	EntityKindComposition = "Composition"
)

// deriveEntities builds the distinct non-Component entities implied by the
// component set: System/Domain (partOf), Group (ownedBy), API (contracts),
// Resource (dependsOn Resource). Deterministic: returned sorted by (kind, key).
func deriveEntities(manifests []ComponentManifest) []Entity {
	type key struct{ kind, k string }
	seen := map[key]*Entity{}
	order := []key{}
	ensure := func(kind, entityKey, name string, spec map[string]any) {
		kk := key{kind, entityKey}
		if e, ok := seen[kk]; ok {
			if e.Spec == nil && spec != nil { // enrich a previously-minimal entity
				e.Spec = spec
			}
			return
		}
		e := &Entity{
			APIVersion: "orun.io/v1",
			Kind:       kind,
			Identity:   EntityIdentity{EntityKey: entityKey, Kind: kind, Name: name},
			Spec:       spec,
		}
		seen[kk] = e
		order = append(order, kk)
	}
	for _, m := range manifests {
		for _, r := range m.Relations {
			switch {
			case r.Type == "partOf" && r.ToKind == EntityKindSystem:
				ensure(EntityKindSystem, r.To, lastSegment(r.To), nil)
			case r.Type == "partOf" && r.ToKind == EntityKindDomain:
				ensure(EntityKindDomain, r.To, lastSegment(r.To), nil)
			case r.Type == "ownedBy" && r.ToKind == EntityKindGroup:
				ensure(EntityKindGroup, r.To, lastSegment(r.To), nil)
			case r.Type == "dependsOn" && r.ToKind == EntityKindResource:
				ensure(EntityKindResource, r.To, lastSegment(r.To), nil)
			case r.Type == "deployedTo" && r.ToKind == EntityKindEnvironment:
				ensure(EntityKindEnvironment, r.To, lastSegment(r.To), nil)
			case r.Type == "composedBy" && r.ToKind == EntityKindComposition:
				ensure(EntityKindComposition, r.To, lastSegment(r.To), compositionSpec(m.Spec))
			}
		}
		for _, api := range contractAPIs(m.Contracts) {
			ensure(EntityKindAPI, api, lastSegment(api), nil)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].kind != order[j].kind {
			return order[i].kind < order[j].kind
		}
		return order[i].k < order[j].k
	})
	out := make([]Entity, 0, len(order))
	for _, kk := range order {
		out = append(out, *seen[kk])
	}
	return out
}

// compositionSpec projects a derived Composition entity's spec from the backing
// component's spec.composition block (the source name + content digest + source
// pointer, data-model.md §5). Returns nil when absent.
func compositionSpec(spec map[string]any) map[string]any {
	c, ok := spec["composition"].(map[string]any)
	if !ok || len(c) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{"source", "digest", "sourceKind", "sourceRef", "sourcePath"} {
		if v, ok := c[k].(string); ok && v != "" {
			out[k] = v
		}
	}
	// SC8: carry the effects declaration (integrations/provides/scorecards) into
	// the Composition entity — declarations only; the live plane records actuals.
	if eff, ok := c["effects"].(map[string]any); ok && len(eff) > 0 {
		out["effects"] = eff
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// contractAPIs returns the api keys named in a manifest's contracts block
// (provides + consumes), tolerating the generic-map shape.
func contractAPIs(contracts map[string]any) []string {
	if contracts == nil {
		return nil
	}
	var out []string
	for _, side := range []string{"provides", "consumes"} {
		list, _ := contracts[side].([]any)
		for _, raw := range list {
			entry, _ := raw.(map[string]any)
			if api, _ := entry["api"].(string); api != "" {
				out = append(out, api)
			}
		}
	}
	return out
}

func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	return s
}

// shortHexTail returns the first n chars of an object id's hex tail (after the
// "<algo>:" prefix), for collision-disambiguating filenames.
func shortHexTail(id string, n int) string {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > n {
		return id[:n]
	}
	return id
}

// assembleEntities writes each derived entity as entities/<Kind>/<name>.json and
// returns the entities/ subtree id plus the per-kind counts. The subtree is
// always written (possibly empty) for a uniform catalog tree shape.
//
// Filenames are the sanitized entity name; when two entities of one kind share
// a sanitized name (e.g. Groups "@org-a/edge" and "@org-b/edge" both → "edge"),
// the colliding entries fall back to the sanitized full entityKey so the tree
// stays valid (duplicate tree names are rejected by the store) and the naming
// stays deterministic.
func assembleEntities(ctx context.Context, s store, entities []Entity) (ObjectID, map[string]int, error) {
	// First pass: count sanitized names per kind to detect collisions.
	nameCount := map[string]int{} // "<kind>/<sanitizedName>" → occurrences
	keyCount := map[string]int{}  // "<kind>/<sanitizedKey>" → occurrences
	for _, e := range entities {
		nameCount[e.Kind+"/"+sanitizeSegment(e.Identity.Name)]++
		keyCount[e.Kind+"/"+sanitizeSegment(e.Identity.EntityKey)]++
	}

	counts := map[string]int{}
	byKind := map[string][]objectstore.TreeEntry{}
	for _, e := range entities {
		e.APIVersion = "orun.io/v1"
		if err := e.Validate(); err != nil {
			return "", nil, err
		}
		b, err := Encode(e)
		if err != nil {
			return "", nil, err
		}
		id, err := s.PutBlob(ctx, b)
		if err != nil {
			return "", nil, err
		}
		fileBase := sanitizeSegment(e.Identity.Name)
		if nameCount[e.Kind+"/"+fileBase] > 1 {
			fileBase = sanitizeSegment(e.Identity.EntityKey)
			if keyCount[e.Kind+"/"+fileBase] > 1 {
				// Even the sanitized keys fold together (e.g. "a/b" vs "a-b"):
				// disambiguate with the content-derived blob id, which is unique
				// per distinct entity and deterministic.
				fileBase += "-" + shortHexTail(string(id), 8)
			}
		}
		byKind[e.Kind] = append(byKind[e.Kind], blobEntry(fileBase+".json", id))
		counts[e.Kind]++
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	kindEntries := make([]objectstore.TreeEntry, 0, len(kinds))
	for _, k := range kinds {
		treeID, err := s.PutTree(ctx, byKind[k])
		if err != nil {
			return "", nil, err
		}
		kindEntries = append(kindEntries, treeEntry(sanitizeSegment(k), treeID))
	}
	treeID, err := s.PutTree(ctx, kindEntries)
	if err != nil {
		return "", nil, err
	}
	return treeID, counts, nil
}

// assembleRelations builds the single typed relation graph (relations.json) from
// the entity relations carried on each manifest and writes it as a blob. The
// forward edge is from the entity to its relation target; edges are sorted by
// (from, fromKind, type, to) for determinism (data-model.md §3). The blob is
// always written (possibly empty) so the catalog tree shape stays uniform.
func assembleRelations(ctx context.Context, s store, manifests []ComponentManifest) (ObjectID, error) {
	var edges []RelationEdge
	for _, m := range manifests {
		// Every resolved kind today is a Component; SC3 carries the entity kind
		// explicitly when other kinds gain first-class entities.
		from := m.Identity.ComponentKey
		for _, r := range m.Relations {
			edges = append(edges, RelationEdge{
				From: from, FromKind: "Component",
				Type: r.Type, To: r.To, ToKind: r.ToKind,
				Optional: r.Optional, Include: r.Include,
			})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.FromKind != b.FromKind {
			return a.FromKind < b.FromKind
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.To < b.To
	})
	rg := RelationGraph{Kind: KindRelationGraph, Edges: edges}
	if err := rg.Validate(); err != nil {
		return "", err
	}
	b, err := Encode(rg)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, b)
}

// assembleImpact writes the impact/ subtree: ownership.json plus the
// fingerprints/ subtree (one blob per component). Both are always written so the
// catalog tree shape stays uniform; Kind/SchemaVersion are defaulted here so
// callers need only supply the derived data.
func assembleImpact(ctx context.Context, s store, ownership ImpactOwnership, fingerprints []ComponentFingerprint) (ObjectID, error) {
	ownership.Kind = KindImpactOwnership
	if ownership.SchemaVersion == 0 {
		ownership.SchemaVersion = 1
	}
	if err := ownership.Validate(); err != nil {
		return "", err
	}
	ob, err := Encode(ownership)
	if err != nil {
		return "", err
	}
	oid, err := s.PutBlob(ctx, ob)
	if err != nil {
		return "", err
	}

	fpTreeID, err := assembleFingerprints(ctx, s, fingerprints)
	if err != nil {
		return "", err
	}
	return s.PutTree(ctx, []objectstore.TreeEntry{
		blobEntry(fileOwnership, oid),
		treeEntry(dirFingerprints, fpTreeID),
	})
}

// assembleFingerprints writes one blob per component fingerprint into the
// fingerprints/ subtree (always present, possibly empty). Kind/SchemaVersion are
// defaulted so callers supply only the derived data.
func assembleFingerprints(ctx context.Context, s store, fingerprints []ComponentFingerprint) (ObjectID, error) {
	entries := make([]objectstore.TreeEntry, 0, len(fingerprints))
	for _, fp := range fingerprints {
		fp.Kind = KindComponentFingerprint
		if fp.SchemaVersion == 0 {
			fp.SchemaVersion = 1
		}
		if err := fp.Validate(); err != nil {
			return "", err
		}
		fb, err := Encode(fp)
		if err != nil {
			return "", err
		}
		fid, err := s.PutBlob(ctx, fb)
		if err != nil {
			return "", err
		}
		// Name by component name (the last componentKey segment), matching the
		// components/<name>.json convention and the data-model §2b filename.
		name := fp.ComponentKey
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		entries = append(entries, blobEntry(sanitizeSegment(name)+".json", fid))
	}
	return s.PutTree(ctx, entries)
}

// NamedBlob is a name→bytes pair for events and artifacts; Name must be in the
// tree-entry alphabet (the caller owns the naming convention, e.g.
// "<seq>-<kind>.json" for events).
type NamedBlob struct {
	Name string
	Data []byte
}

// StepInput / AttemptInput / JobInput / ExecutionInput describe a sealed
// execution tree for AssembleExecution. The runner (M7) populates these from
// its working tree.
type StepInput struct {
	Record StepAttempt
	Log    []byte // optional; stored as a content blob, LogID set from its id
}

type AttemptInput struct {
	Record JobAttempt
	Steps  []StepInput
}

type JobInput struct {
	Record   JobRun
	Attempts []AttemptInput
}

type ExecutionInput struct {
	Execution ExecutionRun
	Jobs      []JobInput
	Events    []NamedBlob
	Artifacts []NamedBlob
}

// AssembleExecution writes a complete sealed execution tree and returns its id
// (Merkle root). The tree shape is execution.json + jobs/ + events/ +
// artifacts/, with each job as jobs/<folder>/{job-run.json, attempts/<n>/
// {attempt.json, steps/s-*.json}}. Child id maps (JobIDs/AttemptIDs/StepIDs)
// are filled from the written children. jobs/events/artifacts subtrees are
// always present (possibly empty) for a uniform shape.
func AssembleExecution(ctx context.Context, s store, in ExecutionInput) (ObjectID, error) {
	exec := in.Execution
	exec.Kind = KindExecutionRun
	exec.JobIDs = make(map[string]string, len(in.Jobs))

	jobEntries := make([]objectstore.TreeEntry, 0, len(in.Jobs))
	for _, j := range in.Jobs {
		jr := j.Record
		jr.Kind = KindJobRun
		jr.AttemptIDs = make(map[string]string, len(j.Attempts))

		attemptEntries := make([]objectstore.TreeEntry, 0, len(j.Attempts))
		for _, a := range j.Attempts {
			att := a.Record
			att.Kind = KindJobAttempt
			att.StepIDs = make(map[string]string, len(a.Steps))

			stepEntries := make([]objectstore.TreeEntry, 0, len(a.Steps))
			for _, st := range a.Steps {
				rec := st.Record
				rec.Kind = KindStepAttempt
				if len(st.Log) > 0 {
					logID, err := s.PutBlob(ctx, st.Log)
					if err != nil {
						return "", err
					}
					rec.LogID = string(logID)
				}
				if err := rec.Validate(); err != nil {
					return "", err
				}
				sb, err := Encode(rec)
				if err != nil {
					return "", err
				}
				sid, err := s.PutBlob(ctx, sb)
				if err != nil {
					return "", err
				}
				stepEntries = append(stepEntries, blobEntry("s-"+sanitizeSegment(rec.StepID)+".json", sid))
				att.StepIDs[rec.StepID] = string(sid)
			}
			stepsTreeID, err := s.PutTree(ctx, stepEntries)
			if err != nil {
				return "", err
			}
			if err := att.Validate(); err != nil {
				return "", err
			}
			ab, err := Encode(att)
			if err != nil {
				return "", err
			}
			attBlobID, err := s.PutBlob(ctx, ab)
			if err != nil {
				return "", err
			}
			attemptTreeID, err := s.PutTree(ctx, []objectstore.TreeEntry{
				blobEntry(fileAttempt, attBlobID),
				treeEntry(dirSteps, stepsTreeID),
			})
			if err != nil {
				return "", err
			}
			n := strconv.Itoa(att.Attempt)
			attemptEntries = append(attemptEntries, treeEntry(n, attemptTreeID))
			jr.AttemptIDs[n] = string(attemptTreeID)
		}
		attemptsTreeID, err := s.PutTree(ctx, attemptEntries)
		if err != nil {
			return "", err
		}
		if err := jr.Validate(); err != nil {
			return "", err
		}
		jb, err := Encode(jr)
		if err != nil {
			return "", err
		}
		jobBlobID, err := s.PutBlob(ctx, jb)
		if err != nil {
			return "", err
		}
		jobTreeID, err := s.PutTree(ctx, []objectstore.TreeEntry{
			blobEntry(fileJobRun, jobBlobID),
			treeEntry(dirAttempts, attemptsTreeID),
		})
		if err != nil {
			return "", err
		}
		jobEntries = append(jobEntries, treeEntry(jr.Folder, jobTreeID))
		exec.JobIDs[jr.Folder] = string(jobTreeID)
	}

	jobsTreeID, err := s.PutTree(ctx, jobEntries)
	if err != nil {
		return "", err
	}
	eventsTreeID, err := putNamedTree(ctx, s, in.Events)
	if err != nil {
		return "", err
	}
	artifactsTreeID, err := putNamedTree(ctx, s, in.Artifacts)
	if err != nil {
		return "", err
	}

	if len(exec.JobIDs) == 0 {
		exec.JobIDs = nil
	}
	if err := exec.Validate(); err != nil {
		return "", err
	}
	eb, err := Encode(exec)
	if err != nil {
		return "", err
	}
	execBlobID, err := s.PutBlob(ctx, eb)
	if err != nil {
		return "", err
	}
	return s.PutTree(ctx, []objectstore.TreeEntry{
		blobEntry(fileExecution, execBlobID),
		treeEntry(dirJobs, jobsTreeID),
		treeEntry(dirEvents, eventsTreeID),
		treeEntry(dirArtifacts, artifactsTreeID),
	})
}

// putNamedTree stores each NamedBlob and returns the tree id grouping them.
func putNamedTree(ctx context.Context, s store, blobs []NamedBlob) (ObjectID, error) {
	entries := make([]objectstore.TreeEntry, 0, len(blobs))
	for _, nb := range blobs {
		id, err := s.PutBlob(ctx, nb.Data)
		if err != nil {
			return "", err
		}
		entries = append(entries, blobEntry(nb.Name, id))
	}
	return s.PutTree(ctx, entries)
}
