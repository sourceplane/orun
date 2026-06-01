package catalogstore

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// ErrInvalidPathInput is returned by every Validate* helper in this file
// on a malformed argument. Wraps errors so callers may use
// errors.Is(err, ErrInvalidPathInput) (and the public path helpers
// surface this same error when they reject input).
var ErrInvalidPathInput = errors.New("catalogstore: invalid path input")

// pathSegmentMaxLen caps the length of any caller-supplied segment that
// becomes a single path component (component names, owners, systems,
// domains, types, branch names, PR identifiers, ref names, graph kinds,
// event kinds). The cap matches typical filesystem NAME_MAX / 2 to leave
// room for trailing extensions and is intentionally larger than any
// valid sanitized branch (40, per identity-and-keys.md §2 rule 2) but
// well below the OS limit so we surface programmer mistakes early.
const pathSegmentMaxLen = 128

// componentKeyMaxLen caps the length of a sanitized componentKey when
// used as a single filename. Matches the path-segment cap with extra
// room for the two segment dashes the sanitizer introduces.
const componentKeyMaxLen = 200

// catalogStoreRoot returns the no-op slash-rooted prefix every helper in
// this file builds upon. Centralized so a future Phase-3 prefix (per
// sync-model.md §3) can be threaded through one site.
func catalogStoreRoot() string { return "" }

// pathJoin glues already-validated segments via path.Join. Callers must
// validate every segment first; this function is only here so we never
// reach for raw string concatenation.
func pathJoin(parts ...string) string {
	root := catalogStoreRoot()
	if root != "" {
		parts = append([]string{root}, parts...)
	}
	return path.Join(parts...)
}

// ValidateSegment is the workhorse alphabet check used by every helper
// whose input is not already validated by a catalogmodel.Validate*
// sibling. Mirrors statestore's per-component policy: non-empty, not
// "." or "..", no '/' or '\' or whitespace, runes in [a-z0-9._-]
// (lowercase only — sanitization to lowercase is the caller's job), and
// length ≤ pathSegmentMaxLen.
func ValidateSegment(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty segment", ErrInvalidPathInput)
	}
	if s == "." || s == ".." {
		return fmt.Errorf("%w: reserved segment %q", ErrInvalidPathInput, s)
	}
	if len(s) > pathSegmentMaxLen {
		return fmt.Errorf("%w: segment %q exceeds %d chars", ErrInvalidPathInput, s, pathSegmentMaxLen)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		case r == '/' || r == '\\':
			return fmt.Errorf("%w: segment %q contains a separator", ErrInvalidPathInput, s)
		default:
			return fmt.Errorf("%w: segment %q contains disallowed character %q", ErrInvalidPathInput, s, r)
		}
	}
	if strings.Contains(s, "..") {
		// belt-and-braces — an entire ".." was rejected above; this
		// catches embedded "x..y" which would resolve cleanly through
		// path.Join but is still surprising and not used anywhere.
		return fmt.Errorf("%w: segment %q contains %q", ErrInvalidPathInput, s, "..")
	}
	return nil
}

// ValidateSourceKey wraps catalogmodel.ValidateSourceSnapshotKey and
// re-types the error under ErrInvalidPathInput so all path-input
// failures share a common sentinel.
func ValidateSourceKey(srcKey string) error {
	if err := catalogmodel.ValidateSourceSnapshotKey(srcKey); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPathInput, err)
	}
	return nil
}

// ValidateCatalogKey wraps catalogmodel.ValidateCatalogSnapshotKey.
func ValidateCatalogKey(catKey string) error {
	if err := catalogmodel.ValidateCatalogSnapshotKey(catKey); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPathInput, err)
	}
	return nil
}

// ValidateComponentName checks that name is a valid single-segment
// component name per identity-and-keys.md §4 (`[a-z0-9._-]+`). Used by
// every helper that takes a `name` parameter.
func ValidateComponentName(name string) error {
	if err := ValidateSegment(name); err != nil {
		return fmt.Errorf("%w (component name)", err)
	}
	return nil
}

// ValidateOwner / ValidateSystem / ValidateDomain / ValidateType are
// thin aliases over ValidateSegment so tests can pin per-helper error
// surfaces and so future tightening (e.g. per-axis caps) lands at one
// site.
func ValidateOwner(owner string) error   { return ValidateSegment(owner) }
func ValidateSystem(system string) error { return ValidateSegment(system) }
func ValidateDomain(domain string) error { return ValidateSegment(domain) }
func ValidateType(typ string) error      { return ValidateSegment(typ) }
func ValidateBranchSeg(branch string) error {
	// Branch names are caller-sanitized via catalogmodel.SanitizeBranch
	// before they reach the path layer; we re-validate the sanitized
	// form against the path alphabet defensively.
	return ValidateSegment(branch)
}

// ValidatePRSeg validates a sanitized PR identifier — typically just
// the integer PR number rendered as decimal digits (`139`). The
// alphabet check tolerates `pr-<num>` style as well; callers may choose
// either convention.
func ValidatePRSeg(pr string) error { return ValidateSegment(pr) }

// allowedRefNames is the closed set of source/catalog ref names that
// SourceRefPath / CatalogRefPath accept directly. Branch- and PR-scoped
// refs go through SourceBranchRefPath / SourcePRRefPath instead.
var allowedRefNames = map[string]struct{}{
	catalogmodel.RefNameLatest:  {},
	catalogmodel.RefNameCurrent: {},
	catalogmodel.RefNameMain:    {},
}

// ValidateRefName accepts only "latest", "current", or "main" — the
// three top-level refs under refs/sources and refs/catalogs.
func ValidateRefName(name string) error {
	if _, ok := allowedRefNames[name]; !ok {
		return fmt.Errorf("%w: ref name %q must be one of latest|current|main", ErrInvalidPathInput, name)
	}
	return nil
}

// allowedGraphKinds is the closed set of catalog-graph filenames
// callers may request. Order matches catalog-store.md §3 step B.2.
var allowedGraphKinds = []string{"dependencies", "systems", "apis", "resources", "owners"}

// CatalogGraphKinds returns the canonical write-order for catalog
// graphs. Exported so the writer (and tests) cite a single source of
// truth.
func CatalogGraphKinds() []string {
	out := make([]string, len(allowedGraphKinds))
	copy(out, allowedGraphKinds)
	return out
}

// ValidateGraphKind ensures kind is one of the five recognized graph
// names.
func ValidateGraphKind(kind string) error {
	for _, k := range allowedGraphKinds {
		if k == kind {
			return nil
		}
	}
	return fmt.Errorf("%w: graph kind %q must be one of %v", ErrInvalidPathInput, kind, allowedGraphKinds)
}

// ValidateEventKind validates an event kind string before
// SanitizeEventKind is applied. Catalog-model EventType constants are
// dotted (e.g. "execution.completed"); we accept dots here and rely on
// catalogmodel.SanitizeEventKind to fold them to dashes for the
// filename.
func ValidateEventKind(kind string) error {
	if kind == "" {
		return fmt.Errorf("%w: event kind is empty", ErrInvalidPathInput)
	}
	if len(kind) > pathSegmentMaxLen {
		return fmt.Errorf("%w: event kind %q exceeds %d chars", ErrInvalidPathInput, kind, pathSegmentMaxLen)
	}
	if strings.ContainsAny(kind, "/\\ \t\r\n") {
		return fmt.Errorf("%w: event kind %q contains a separator or whitespace", ErrInvalidPathInput, kind)
	}
	if strings.Contains(kind, "..") {
		return fmt.Errorf("%w: event kind %q contains %q", ErrInvalidPathInput, kind, "..")
	}
	for _, r := range kind {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return fmt.Errorf("%w: event kind %q contains disallowed character %q", ErrInvalidPathInput, kind, r)
		}
	}
	return nil
}

// ValidateRevisionKey validates a revision key as a path segment. The
// shape rule lives in internal/revision (Phase 1); we apply the path
// alphabet here so a caller passing nonsense gets the same surface as
// every other ValidateSegment caller.
func ValidateRevisionKey(revKey string) error { return ValidateSegment(revKey) }

// ValidateExecutionKey validates an execution key the same way.
func ValidateExecutionKey(execKey string) error { return ValidateSegment(execKey) }

// ----- Path helpers: source/catalog directory tree --------------------

// SourceDir returns "sources/<srcKey>".
func SourceDir(srcKey string) (string, error) {
	if err := ValidateSourceKey(srcKey); err != nil {
		return "", err
	}
	return pathJoin("sources", srcKey), nil
}

// SourceDocPath returns "sources/<srcKey>/source.json".
func SourceDocPath(srcKey string) (string, error) {
	dir, err := SourceDir(srcKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "source.json"), nil
}

// CatalogDir returns "sources/<srcKey>/catalogs/<catKey>".
func CatalogDir(srcKey, catKey string) (string, error) {
	if err := ValidateSourceKey(srcKey); err != nil {
		return "", err
	}
	if err := ValidateCatalogKey(catKey); err != nil {
		return "", err
	}
	return pathJoin("sources", srcKey, "catalogs", catKey), nil
}

// CatalogDocPath returns ".../catalogs/<catKey>/catalog.json".
func CatalogDocPath(srcKey, catKey string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "catalog.json"), nil
}

// ComponentDir returns ".../catalogs/<catKey>/components/<name>".
func ComponentDir(srcKey, catKey, name string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateComponentName(name); err != nil {
		return "", err
	}
	return pathJoin(dir, "components", name), nil
}

// ComponentManifestPath returns ".../components/<name>/manifest.json".
func ComponentManifestPath(srcKey, catKey, name string) (string, error) {
	dir, err := ComponentDir(srcKey, catKey, name)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "manifest.json"), nil
}

// CatalogGraphPath returns ".../catalogs/<catKey>/graph/<kind>.json".
func CatalogGraphPath(srcKey, catKey, kind string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateGraphKind(kind); err != nil {
		return "", err
	}
	return pathJoin(dir, "graph", kind+".json"), nil
}

// ----- Path helpers: revisions / executions ---------------------------

// CatalogRevisionDir returns ".../catalogs/<catKey>/revisions/<revKey>".
func CatalogRevisionDir(srcKey, catKey, revKey string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateRevisionKey(revKey); err != nil {
		return "", err
	}
	return pathJoin(dir, "revisions", revKey), nil
}

// CatalogRevisionPlanPath returns ".../revisions/<revKey>/plan.json".
func CatalogRevisionPlanPath(srcKey, catKey, revKey string) (string, error) {
	dir, err := CatalogRevisionDir(srcKey, catKey, revKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "plan.json"), nil
}

// CatalogRevisionTriggerPath returns ".../revisions/<revKey>/trigger.json".
func CatalogRevisionTriggerPath(srcKey, catKey, revKey string) (string, error) {
	dir, err := CatalogRevisionDir(srcKey, catKey, revKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "trigger.json"), nil
}

// CatalogRevisionDocPath returns ".../revisions/<revKey>/revision.json".
func CatalogRevisionDocPath(srcKey, catKey, revKey string) (string, error) {
	dir, err := CatalogRevisionDir(srcKey, catKey, revKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "revision.json"), nil
}

// CatalogRevisionManifestPath returns ".../revisions/<revKey>/manifest.json".
func CatalogRevisionManifestPath(srcKey, catKey, revKey string) (string, error) {
	dir, err := CatalogRevisionDir(srcKey, catKey, revKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "manifest.json"), nil
}

// CatalogExecutionDir returns
// ".../revisions/<revKey>/executions/<execKey>".
func CatalogExecutionDir(srcKey, catKey, revKey, execKey string) (string, error) {
	dir, err := CatalogRevisionDir(srcKey, catKey, revKey)
	if err != nil {
		return "", err
	}
	if err := ValidateExecutionKey(execKey); err != nil {
		return "", err
	}
	return pathJoin(dir, "executions", execKey), nil
}

// CatalogExecutionDocPath returns
// ".../revisions/<revKey>/executions/<execKey>/execution.json".
func CatalogExecutionDocPath(srcKey, catKey, revKey, execKey string) (string, error) {
	dir, err := CatalogExecutionDir(srcKey, catKey, revKey, execKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, "execution.json"), nil
}

// CatalogExecutionFilePath returns
// ".../revisions/<revKey>/executions/<execKey>/<name>".
func CatalogExecutionFilePath(srcKey, catKey, revKey, execKey, name string) (string, error) {
	dir, err := CatalogExecutionDir(srcKey, catKey, revKey, execKey)
	if err != nil {
		return "", err
	}
	return pathJoin(dir, name), nil
}

// ----- Path helpers: refs ---------------------------------------------

// SourceRefPath returns "refs/sources/<name>.json" where name is one of
// latest|current|main.
func SourceRefPath(name string) (string, error) {
	if err := ValidateRefName(name); err != nil {
		return "", err
	}
	return pathJoin("refs", "sources", name+".json"), nil
}

// SourceBranchRefPath returns "refs/sources/branches/<branch>.json".
// `branch` is expected to already be sanitized via
// catalogmodel.SanitizeBranch.
func SourceBranchRefPath(branch string) (string, error) {
	if err := ValidateBranchSeg(branch); err != nil {
		return "", err
	}
	return pathJoin("refs", "sources", "branches", branch+".json"), nil
}

// SourcePRRefPath returns "refs/sources/prs/<pr>.json".
func SourcePRRefPath(pr string) (string, error) {
	if err := ValidatePRSeg(pr); err != nil {
		return "", err
	}
	return pathJoin("refs", "sources", "prs", pr+".json"), nil
}

// CatalogRefPath returns "refs/catalogs/<name>.json".
func CatalogRefPath(name string) (string, error) {
	if err := ValidateRefName(name); err != nil {
		return "", err
	}
	return pathJoin("refs", "catalogs", name+".json"), nil
}

// CatalogBranchRefPath returns "refs/catalogs/branches/<branch>.json".
func CatalogBranchRefPath(branch string) (string, error) {
	if err := ValidateBranchSeg(branch); err != nil {
		return "", err
	}
	return pathJoin("refs", "catalogs", "branches", branch+".json"), nil
}

// CatalogPRRefPath returns "refs/catalogs/prs/<pr>.json".
func CatalogPRRefPath(pr string) (string, error) {
	if err := ValidatePRSeg(pr); err != nil {
		return "", err
	}
	return pathJoin("refs", "catalogs", "prs", pr+".json"), nil
}

// ----- Path helpers: catalog-local indexes ----------------------------

// ComponentLocalIndexPath returns
// ".../catalogs/<catKey>/indexes/components/<name>.json".
func ComponentLocalIndexPath(srcKey, catKey, name string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateComponentName(name); err != nil {
		return "", err
	}
	return pathJoin(dir, "indexes", "components", name+".json"), nil
}

// OwnerLocalIndexPath returns
// ".../catalogs/<catKey>/indexes/owners/<owner>.json".
func OwnerLocalIndexPath(srcKey, catKey, owner string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateOwner(owner); err != nil {
		return "", err
	}
	return pathJoin(dir, "indexes", "owners", owner+".json"), nil
}

// SystemLocalIndexPath returns
// ".../catalogs/<catKey>/indexes/systems/<system>.json".
func SystemLocalIndexPath(srcKey, catKey, system string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateSystem(system); err != nil {
		return "", err
	}
	return pathJoin(dir, "indexes", "systems", system+".json"), nil
}

// DomainLocalIndexPath returns
// ".../catalogs/<catKey>/indexes/domains/<domain>.json".
func DomainLocalIndexPath(srcKey, catKey, domain string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateDomain(domain); err != nil {
		return "", err
	}
	return pathJoin(dir, "indexes", "domains", domain+".json"), nil
}

// TypeLocalIndexPath returns
// ".../catalogs/<catKey>/indexes/types/<type>.json".
func TypeLocalIndexPath(srcKey, catKey, typ string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateType(typ); err != nil {
		return "", err
	}
	return pathJoin(dir, "indexes", "types", typ+".json"), nil
}

// ----- Path helpers: global indexes -----------------------------------

// ComponentGlobalIndexPath returns
// "indexes/components/<sanitizedComponentKey>.json". Slashes in the
// 3-segment componentKey are folded via catalogmodel.SanitizeComponentKey.
func ComponentGlobalIndexPath(componentKey string) (string, error) {
	if err := catalogmodel.ValidateComponentKey(componentKey); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPathInput, err)
	}
	sanitized := catalogmodel.SanitizeComponentKey(componentKey)
	if sanitized == "" {
		return "", fmt.Errorf("%w: sanitized componentKey is empty", ErrInvalidPathInput)
	}
	if len(sanitized) > componentKeyMaxLen {
		return "", fmt.Errorf("%w: sanitized componentKey %q exceeds %d chars", ErrInvalidPathInput, sanitized, componentKeyMaxLen)
	}
	return pathJoin("indexes", "components", sanitized+".json"), nil
}

// CatalogGlobalIndexPath returns "indexes/catalogs/<catKey>.json".
func CatalogGlobalIndexPath(catKey string) (string, error) {
	if err := ValidateCatalogKey(catKey); err != nil {
		return "", err
	}
	return pathJoin("indexes", "catalogs", catKey+".json"), nil
}

// SourceGlobalIndexPath returns "indexes/sources/<srcKey>.json".
func SourceGlobalIndexPath(srcKey string) (string, error) {
	if err := ValidateSourceKey(srcKey); err != nil {
		return "", err
	}
	return pathJoin("indexes", "sources", srcKey+".json"), nil
}

// ----- Path helpers: history events -----------------------------------

// ComponentHistoryEventPath returns
// ".../history/components/<name>/events/<seq:09d>-<sanitizedKind>.json".
// Seq is rendered zero-padded to 9 digits per catalog-store.md §2.
func ComponentHistoryEventPath(srcKey, catKey, name string, seq uint64, kind string) (string, error) {
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return "", err
	}
	if err := ValidateComponentName(name); err != nil {
		return "", err
	}
	if err := ValidateEventKind(kind); err != nil {
		return "", err
	}
	sanitizedKind := catalogmodel.SanitizeEventKind(kind)
	if sanitizedKind == "" {
		return "", fmt.Errorf("%w: sanitized event kind is empty", ErrInvalidPathInput)
	}
	filename := fmt.Sprintf("%09d-%s.json", seq, sanitizedKind)
	return pathJoin(dir, "history", "components", name, "events", filename), nil
}
