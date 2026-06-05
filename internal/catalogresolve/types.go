package catalogresolve

import (
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// AuthoredManifest is the discover+load+inherit output for a single
// component.yaml file. It carries the resolved authored shape, the source
// file the manifest was read from (workspace-relative, slash-separated),
// and the provenance trail recording where every authored-or-inherited
// field came from.
//
// Fields that the C2 second PR (infer + deps + validate + hash) needs to
// attribute back to source MUST be recorded in Provenance — see §10
// (manifestHash) and resolution-pipeline.md §3 for the inheritance
// precedence rules.
type AuthoredManifest struct {
	// SourceFile is the workspace-relative path of the authored
	// component.yaml (or component.yml) file, slash-separated and
	// case-preserved. Walks emit a deterministic ordering of these.
	SourceFile string

	// Component is the authored manifest after schema validation and
	// after the intent.yaml `catalog.defaults` layer has been applied
	// underneath any explicit authored values. The shape mirrors
	// catalogmodel.ComponentYAML byte-for-byte; the canonical-encoder
	// invariant (sorted-map encoding for hash inputs) is preserved by
	// using the catalogmodel types directly.
	Component catalogmodel.ComponentYAML

	// Provenance maps a JSON-pointer-style field path (e.g.
	// "metadata.labels.repo", "spec.lifecycle") to a Provenance entry
	// describing which file and which JSON pointer that value originated
	// from. Every field that is non-zero in Component MUST have a
	// Provenance entry. Inheritance fills entries from the intent file
	// when authored leaves the field unset.
	Provenance map[string]Provenance

	// UnknownFields lists RFC 6901 pointers to authored keys the catalog
	// does not recognize (e.g. "/spec/inputs"). The authoring schema is
	// deliberately open so such keys do not fail validation, but the
	// resolver surfaces them as warnings so typos and dropped legacy
	// fields stay observable. See unknownFields. Sorted, deterministic.
	UnknownFields []string
}

// Provenance attributes a single resolved field to its origin. File is
// workspace-relative, slash-separated. Pointer is a JSON pointer per
// RFC 6901 (e.g. "/metadata/labels/repo", "/spec/lifecycle").
type Provenance struct {
	File    string
	Pointer string
}

// DiscoveryResult is the deterministic output of a DiscoverAndLoad call.
// Manifests is sorted lexically by SourceFile.
type DiscoveryResult struct {
	// Manifests is the resolved authored manifests, lexically sorted by
	// SourceFile. Empty when no component.yaml files exist.
	Manifests []AuthoredManifest

	// IntentPath is the workspace-relative path to the intent file that
	// supplied catalog defaults, or "" when none was loaded.
	IntentPath string
}

// ResolvedCatalog is the output of the full Resolve pipeline (stages
// 1–10). It carries the resolved per-component manifests with their
// computed manifestHash plus the validation issues collected during
// the run. The caller (writer at C4) consumes this verbatim — no
// further computation required.
//
// ResolvedCatalog is deterministic: two consecutive Resolve calls
// against the same workspace produce a byte-identical encoding (T-RES-1).
type ResolvedCatalog struct {
	// Manifests is the resolved component set, ordered by
	// Identity.ComponentKey. Each entry has Source.ManifestHash filled
	// in.
	Manifests []*catalogmodel.ComponentManifest

	// Issues is the collected validation issues from stage 9, ordered
	// by (severity desc, code, file, pointer). May be empty.
	Issues []ValidationIssue

	// IntentPath is the workspace-relative path of the intent file used
	// for inheritance, or "" when none.
	IntentPath string

	// Namespace is the effective namespace used for componentKey
	// construction.
	Namespace string

	// Repo is the effective repo segment used for componentKey
	// construction.
	Repo string

	// Excludes is the sorted, de-duplicated directory-basename prune set
	// discovery used (defaults ∪ intent catalog.discovery.exclude). The
	// change-detection ownership map mirrors it as ignoreDirs.
	Excludes []string
}

// Options configures DiscoverAndLoad and Resolve. Exactly one of
// WorkspaceRoot must be set; IntentPath defaults to
// "<WorkspaceRoot>/intent.yaml" and is optional (a missing intent file
// is not an error — the discover+load pipeline simply has no defaults
// to apply).
type Options struct {
	// WorkspaceRoot is the absolute path to the workspace root the
	// resolver walks. Must exist and be a directory.
	WorkspaceRoot string

	// IntentPath is the absolute path to intent.yaml. When empty,
	// resolves to filepath.Join(WorkspaceRoot, "intent.yaml"). A missing
	// file is OK; a present-but-malformed file is a typed error.
	IntentPath string

	// Strict shifts validation severities per resolution-pipeline.md §6.
	// In strict mode every "warn" becomes "error" and the resolver
	// aborts on the first non-fatal validation issue.
	Strict bool

	// Repo is the workspace's Git repo short name used for
	// componentKey construction (`<namespace>/<repo>/<name>`). When
	// empty, the resolver falls back to filepath.Base(WorkspaceRoot).
	// Cross-repo dependency keys (`<namespace>/<otherRepo>/<name>`)
	// are still permitted in spec.dependsOn but resolve against the
	// discovered set only when the namespace+repo+name triple matches
	// a workspace component.
	Repo string

	// Namespace overrides intent.catalog.namespace. When both are
	// unset the namespace defaults to "default".
	Namespace string

	// Clock is the time seam — the resolver itself does not call
	// time.Now; reserved for inference layers that want to stamp
	// scanned-at timestamps. Nil → defaultClock(); never panics.
	Clock Clock
}

// ErrManifestInvalid is returned when an authored component.yaml fails
// JSON-Schema validation against catalogmodel.ComponentYAMLSchema. File
// is workspace-relative; Pointer is the first failing schema location
// (RFC 6901). Reason carries the underlying validator message.
type ErrManifestInvalid struct {
	File    string
	Pointer string
	Reason  string
}

func (e *ErrManifestInvalid) Error() string {
	if e.Pointer == "" {
		return fmt.Sprintf("catalogresolve: manifest %s invalid: %s", e.File, e.Reason)
	}
	return fmt.Sprintf("catalogresolve: manifest %s invalid at %s: %s", e.File, e.Pointer, e.Reason)
}

// ErrManifestMixedExtension is returned when a single directory contains
// both component.yaml and component.yml. Per
// resolution-pipeline.md §2 this is a validation error.
type ErrManifestMixedExtension struct {
	Dir   string
	Paths []string // both workspace-relative paths, sorted
}

func (e *ErrManifestMixedExtension) Error() string {
	return fmt.Sprintf("catalogresolve: directory %s has both component.yaml and component.yml: %v", e.Dir, e.Paths)
}

// ErrIntentInvalid is returned when intent.yaml exists but is malformed
// or the `catalog` block fails decode.
type ErrIntentInvalid struct {
	File   string
	Reason string
}

func (e *ErrIntentInvalid) Error() string {
	return fmt.Sprintf("catalogresolve: intent %s invalid: %s", e.File, e.Reason)
}

// ErrWorkspaceInvalid is returned for top-level configuration problems
// (missing root, root is a file, etc.).
type ErrWorkspaceInvalid struct {
	Reason string
}

func (e *ErrWorkspaceInvalid) Error() string {
	return "catalogresolve: workspace invalid: " + e.Reason
}

// errInternal wraps unexpected resolver failures (e.g. schema compile
// errors). Surfaces as a typed error so the caller can distinguish bugs
// from authoring errors.
type errInternal struct {
	Stage string
	Err   error
}

func (e *errInternal) Error() string {
	return fmt.Sprintf("catalogresolve: internal error in stage %s: %v", e.Stage, e.Err)
}
func (e *errInternal) Unwrap() error { return e.Err }

// errSentinels keeps the package's exported sentinel errors discoverable.
var (
	errEmptyRoot = errors.New("WorkspaceRoot must be set")
)
