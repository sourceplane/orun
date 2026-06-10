package nodes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/objectstore"
)

// ErrInvalid is the validation error sentinel, shared with the object store so
// callers route on one taxonomy.
var ErrInvalid = objectstore.ErrInvalid

// componentKeyRe matches "<namespace>/<repo>/<componentName>" with each segment
// in the lowercase id alphabet. Environment is never part of identity.
var componentKeyRe = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`)

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

func validID(s string) bool { return objectstore.ValidateID(objectstore.ObjectID(s)) == nil }

// Validate checks a SourceSnapshot.
func (s SourceSnapshot) Validate() error {
	if s.Kind != KindSourceSnapshot {
		return invalidf("source kind %q", s.Kind)
	}
	switch s.Scope {
	case ScopeMain, ScopeBranch, ScopePR, ScopeLocalNoGit:
	default:
		return invalidf("source scope %q", s.Scope)
	}
	return nil
}

// Validate checks a CatalogSnapshot (and its component refs).
func (c CatalogSnapshot) Validate() error {
	if c.Kind != KindCatalogSnapshot {
		return invalidf("catalog kind %q", c.Kind)
	}
	if !validID(c.SourceID) {
		return invalidf("catalog sourceId %q", c.SourceID)
	}
	if c.ComponentCount != len(c.Components) {
		return invalidf("catalog componentCount %d != %d components", c.ComponentCount, len(c.Components))
	}
	for _, ref := range c.Components {
		if !componentKeyRe.MatchString(ref.ComponentKey) {
			return invalidf("catalog component key %q", ref.ComponentKey)
		}
		if !validID(ref.ManifestID) {
			return invalidf("catalog manifestId %q", ref.ManifestID)
		}
	}
	return nil
}

// Validate checks a ComponentManifest, including the componentKey shape and the
// name == last-segment rule.
func (m ComponentManifest) Validate() error {
	if m.Kind != KindComponentManifest {
		return invalidf("manifest kind %q", m.Kind)
	}
	if !componentKeyRe.MatchString(m.Identity.ComponentKey) {
		return invalidf("manifest componentKey %q", m.Identity.ComponentKey)
	}
	segs := strings.Split(m.Identity.ComponentKey, "/")
	if m.Identity.Name != segs[len(segs)-1] {
		return invalidf("manifest name %q != last segment of %q", m.Identity.Name, m.Identity.ComponentKey)
	}
	return nil
}

// Validate checks a CatalogGraph.
func (g CatalogGraph) Validate() error {
	if g.Kind != KindCatalogGraph {
		return invalidf("graph kind %q", g.Kind)
	}
	if g.EdgeKind == "" {
		return invalidf("graph edgeKind empty")
	}
	return nil
}

// Validate checks a RelationGraph: kind discriminator plus non-empty endpoints
// and type on every edge (orun-service-catalog/data-model.md §3).
func (g RelationGraph) Validate() error {
	if g.Kind != KindRelationGraph {
		return invalidf("relationGraph kind %q", g.Kind)
	}
	for _, e := range g.Edges {
		if e.From == "" || e.To == "" || e.Type == "" {
			return invalidf("relationGraph edge %q→[%s]→%q has empty endpoint/type", e.From, e.Type, e.To)
		}
	}
	return nil
}

// Validate checks an ImpactOwnership (data-model.md §5). The component map keys
// must be clean workspace-relative directories and the values valid component
// keys; schemaVersion must be present so a cross-version consumer can reject an
// index it does not understand.
func (o ImpactOwnership) Validate() error {
	if o.Kind != KindImpactOwnership {
		return invalidf("ownership kind %q", o.Kind)
	}
	if o.SchemaVersion < 1 {
		return invalidf("ownership schemaVersion %d", o.SchemaVersion)
	}
	for dir, key := range o.Components {
		if !validOwnershipDir(dir) {
			return invalidf("ownership component dir %q", dir)
		}
		if !componentKeyRe.MatchString(key) {
			return invalidf("ownership component key %q", key)
		}
	}
	return nil
}

// validOwnershipDir reports whether dir is a clean workspace-relative path: a
// non-empty slash-separated path (or "." for the workspace root) with no
// leading "./", no trailing "/", no empty or "." / ".." segments.
func validOwnershipDir(dir string) bool {
	if dir == "" {
		return false
	}
	if dir == "." { // the workspace root (a root-authored component)
		return true
	}
	if strings.HasPrefix(dir, "/") || strings.HasSuffix(dir, "/") || strings.HasPrefix(dir, "./") {
		return false
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Validate checks a ComponentFingerprint (data-model.md §2b). The dir must be a
// clean workspace-relative path, the componentKey valid, and schemaVersion ≥ 1.
func (f ComponentFingerprint) Validate() error {
	if f.Kind != KindComponentFingerprint {
		return invalidf("fingerprint kind %q", f.Kind)
	}
	if f.SchemaVersion < 1 {
		return invalidf("fingerprint schemaVersion %d", f.SchemaVersion)
	}
	if !componentKeyRe.MatchString(f.ComponentKey) {
		return invalidf("fingerprint componentKey %q", f.ComponentKey)
	}
	if !validOwnershipDir(f.Dir) {
		return invalidf("fingerprint dir %q", f.Dir)
	}
	if f.Subtree == "" {
		return invalidf("fingerprint %q: empty subtree", f.ComponentKey)
	}
	return nil
}

// Validate checks a PlanRevision. It enforces the identity-purity rule by
// construction: the struct carries no trigger/timestamp/executionId field, so
// an identical plan under different triggers yields identical bytes.
func (r PlanRevision) Validate() error {
	if r.Kind != KindPlanRevision {
		return invalidf("revision kind %q", r.Kind)
	}
	if !validID(r.PlanHash) {
		return invalidf("revision planHash %q", r.PlanHash)
	}
	switch r.Scope.Mode {
	case "full", "changed":
	default:
		return invalidf("revision scope mode %q", r.Scope.Mode)
	}
	if r.CatalogID != "" && !validID(r.CatalogID) {
		return invalidf("revision catalogId %q", r.CatalogID)
	}
	return nil
}

// Validate checks a TriggerOccurrence.
func (t TriggerOccurrence) Validate() error {
	if t.Kind != KindTriggerOccurrence {
		return invalidf("trigger kind %q", t.Kind)
	}
	if !strings.HasPrefix(t.TriggerID, "trg_") {
		return invalidf("trigger id %q lacks trg_ prefix", t.TriggerID)
	}
	if !validID(t.RevisionID) {
		return invalidf("trigger revisionId %q", t.RevisionID)
	}
	if t.TriggerName == "" {
		return invalidf("trigger name empty")
	}
	if t.CreatedAt.IsZero() {
		return invalidf("trigger createdAt zero")
	}
	return nil
}

// Validate checks an ExecutionRun.
func (e ExecutionRun) Validate() error {
	if e.Kind != KindExecutionRun {
		return invalidf("execution kind %q", e.Kind)
	}
	if e.ExecutionID == "" {
		return invalidf("execution id empty")
	}
	if !validID(e.RevisionID) {
		return invalidf("execution revisionId %q", e.RevisionID)
	}
	if e.TriggerID != "" && !strings.HasPrefix(e.TriggerID, "trg_") {
		return invalidf("execution triggerId %q lacks trg_ prefix", e.TriggerID)
	}
	if !validStatus(e.Status) {
		return invalidf("execution status %q", e.Status)
	}
	return nil
}

// Validate checks a JobRun.
func (j JobRun) Validate() error {
	if j.Kind != KindJobRun {
		return invalidf("jobRun kind %q", j.Kind)
	}
	if j.JobID == "" || j.Folder == "" {
		return invalidf("jobRun jobId/folder empty")
	}
	if !validStatus(j.Status) {
		return invalidf("jobRun status %q", j.Status)
	}
	return nil
}

// Validate checks a JobAttempt.
func (a JobAttempt) Validate() error {
	if a.Kind != KindJobAttempt {
		return invalidf("attempt kind %q", a.Kind)
	}
	if a.Attempt < 1 {
		return invalidf("attempt number %d", a.Attempt)
	}
	if !validStatus(a.Status) {
		return invalidf("attempt status %q", a.Status)
	}
	return nil
}

// Validate checks a StepAttempt.
func (s StepAttempt) Validate() error {
	if s.Kind != KindStepAttempt {
		return invalidf("step kind %q", s.Kind)
	}
	if s.StepID == "" {
		return invalidf("step id empty")
	}
	if !validStatus(s.Status) {
		return invalidf("step status %q", s.Status)
	}
	if s.LogID != "" && !validID(s.LogID) {
		return invalidf("step logId %q", s.LogID)
	}
	return nil
}
