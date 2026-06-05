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

	cat.Components = refs
	cat.ComponentCount = len(refs)
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
	impactTreeID, err := assembleImpact(ctx, s, ownership, fingerprints)
	if err != nil {
		return "", err
	}
	return s.PutTree(ctx, []objectstore.TreeEntry{
		blobEntry(fileCatalog, catBlobID),
		treeEntry(dirComponents, compTreeID),
		treeEntry(dirGraph, graphTreeID),
		treeEntry(dirImpact, impactTreeID),
	})
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
