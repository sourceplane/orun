package catalogresolve

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// intentFile mirrors only the catalog-relevant slice of intent.yaml that
// the resolver consumes. Other fields are ignored.
//
// The shape mirrors data-model.md §7. `omitempty` is intentionally absent
// on every field so that yaml.v3 can distinguish "key not present" from
// "key set to empty" via an explicit pointer/length check on the loader
// side (see intentDefaults below).
type intentFile struct {
	Metadata *intentMetadata     `yaml:"metadata"`
	Catalog  *intentCatalogBlock `yaml:"catalog"`
	// Discovery is the top-level planner discovery block. Its roots bound
	// catalog discovery too (see discoveryRoots): the catalog and the planner
	// agree on where component manifests live.
	Discovery *intentRootDiscovery `yaml:"discovery"`
	// Extends lists the intent presets this file inherits. Presets may
	// contribute discovery.roots the resolver cannot see (they are merged by
	// the planner's loader, not here), so a file with presets keeps the
	// whole-workspace walk. Decoded as a raw node so any authored shape is
	// tolerated.
	Extends yaml.Node `yaml:"extends"`
	// Components are the inline component declarations. The catalog ingests
	// these alongside discovered component.yaml files so the component set
	// matches the legacy inline∪discovered set (orun-catalog-state CS5/CS6).
	Components []inlineComponent `yaml:"components"`
	// Repo is the optional top-level block that self-describes THIS repo
	// (saas-workspace-overview WO3). When present, the resolver emits a single
	// Repo entity; when absent, the entity is still emitted from metadata
	// (name, description) and a root README.md when one exists.
	Repo *intentRepoBlock `yaml:"repo"`
	// Environments is the catalog-relevant slice of the declared environments:
	// a description, the front door (url), further links, and the promotion
	// chain (for the entity's order). An environment named here is DECLARED,
	// so it materializes as an Environment entity even before a component
	// deploys to it.
	Environments map[string]*intentEnvironment `yaml:"environments"`
}

// intentEnvironment mirrors the catalog-relevant keys of one
// `environments.<name>` block (model.Environment carries the rest).
type intentEnvironment struct {
	Description string           `yaml:"description"`
	URL         string           `yaml:"url"`
	Links       []intentRepoLink `yaml:"links"`
	Promotion   *intentPromotion `yaml:"promotion"`
}

type intentPromotion struct {
	DependsOn []intentPromotionDependency `yaml:"dependsOn"`
}

type intentPromotionDependency struct {
	Environment string `yaml:"environment"`
}

// intentMetadata mirrors intent.yaml's top-level metadata (name/description are
// used to default the Repo entity's identity + description).
type intentMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Namespace   string `yaml:"namespace"`
}

// intentRepoBlock mirrors the top-level `repo:` block (model.md §2b).
type intentRepoBlock struct {
	DisplayName string           `yaml:"displayName"`
	Description string           `yaml:"description"`
	Owner       string           `yaml:"owner"`
	Docs        *intentRepoDocs  `yaml:"docs"`
	Links       []intentRepoLink `yaml:"links"`
	Tags        []string         `yaml:"tags"`
}

// intentRepoDocs mirrors the repo block's docs: the reserved overview plus the
// ordered pages set (saas-catalog-docs CD1). catalogmodel.DocPage decodes
// directly — yaml.v3's default key mapping (lowercased field name) matches the
// declared keys (path/key/title/role) exactly.
type intentRepoDocs struct {
	Overview string                 `yaml:"overview"`
	Pages    []catalogmodel.DocPage `yaml:"pages"`
}

type intentRepoLink struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
	Icon  string `yaml:"icon"`
}

type intentCatalogBlock struct {
	Namespace string                 `yaml:"namespace"`
	Defaults  *intentCatalogDefaults `yaml:"defaults"`
	Discovery *intentDiscovery       `yaml:"discovery"`
	Inference *intentInference       `yaml:"inference"`
	// Entities is the enrichment block (saas-catalog-docs CD2): metadata +
	// docs merged onto entities the resolver DERIVES (System/Domain/Group/
	// Environment), keyed "<kind>/<name>" with a lowercase kind. Enrichment
	// never creates an entity — a target that doesn't materialize is a
	// warning; an enrichment for a declared kind (component/repo/…) is an
	// error (one declaration site per entity).
	Entities map[string]*intentEntityEnrichment `yaml:"entities"`
}

// intentEntityEnrichment is one catalog.entities value (CD2).
type intentEntityEnrichment struct {
	Description string           `yaml:"description"`
	Owner       string           `yaml:"owner"`
	Links       []intentRepoLink `yaml:"links"`
	Tags        []string         `yaml:"tags"`
	Docs        *intentRepoDocs  `yaml:"docs"`
}

// intentInference mirrors the catalog.inference block of intent.yaml
// per resolution-pipeline.md §4. Pointer fields distinguish "absent"
// from "explicitly false"; absent toggles default to TRUE when
// catalog.inference.enabled is on.
type intentInference struct {
	Enabled     *bool `yaml:"enabled"`
	PackageJSON *bool `yaml:"packageJson"`
	Dockerfile  *bool `yaml:"dockerfile"`
	Terraform   *bool `yaml:"terraform"`
	Helm        *bool `yaml:"helm"`
	Readme      *bool `yaml:"readme"`
}

type intentCatalogDefaults struct {
	Lifecycle   string            `yaml:"lifecycle"`
	Owner       string            `yaml:"owner"`
	System      string            `yaml:"system"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
	Tags        []string          `yaml:"tags"`
	// Tags is reserved here for forward-compat with the spec's
	// `metadata.tags` list. ComponentYAML in this package does not
	// expose a Tags field yet (see catalogmodel/component_yaml.go);
	// inheritance for tags is therefore a no-op until the model gains
	// the field. Keeping the YAML shape tolerant means later additions
	// don't break existing intent files.
}

type intentDiscovery struct {
	Exclude []string `yaml:"exclude"`
}

// intentRootDiscovery mirrors the top-level `discovery:` block the planner's
// loader reads (internal/loader, model.Discovery).
type intentRootDiscovery struct {
	Roots []string `yaml:"roots"`
}

// hasExtends reports whether the intent inherits at least one preset.
func (f *intentFile) hasExtends() bool {
	if f == nil {
		return false
	}
	switch f.Extends.Kind {
	case 0:
		return false
	case yaml.SequenceNode:
		return len(f.Extends.Content) > 0
	case yaml.ScalarNode:
		return f.Extends.Tag != "!!null" && f.Extends.Value != ""
	default:
		return true
	}
}

// discoveryExcludes returns intent.yaml's catalog.discovery.exclude list, or
// nil when the block is absent.
func (f *intentFile) discoveryExcludes() []string {
	if f == nil || f.Catalog == nil || f.Catalog.Discovery == nil {
		return nil
	}
	return f.Catalog.Discovery.Exclude
}

// loadIntent reads the intent file at absPath, returning a parsed shape.
// A non-existent file is NOT an error — the caller distinguishes "file
// missing" from "file present-but-malformed" via the returned (nil, nil)
// vs (nil, *ErrIntentInvalid) cases.
//
// rel is the workspace-relative slash path used in error messages and
// provenance.
func loadIntent(absPath, rel string) (*intentFile, error) {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &ErrIntentInvalid{File: rel, Reason: fmt.Sprintf("read: %v", err)}
	}
	var f intentFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, &ErrIntentInvalid{File: rel, Reason: fmt.Sprintf("yaml decode: %v", err)}
	}
	return &f, nil
}
