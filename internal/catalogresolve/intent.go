package catalogresolve

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// intentFile mirrors only the catalog-relevant slice of intent.yaml that
// the resolver consumes. Other fields are ignored.
//
// The shape mirrors data-model.md §7. `omitempty` is intentionally absent
// on every field so that yaml.v3 can distinguish "key not present" from
// "key set to empty" via an explicit pointer/length check on the loader
// side (see intentDefaults below).
type intentFile struct {
	Catalog *intentCatalogBlock `yaml:"catalog"`
}

type intentCatalogBlock struct {
	Namespace string                 `yaml:"namespace"`
	Defaults  *intentCatalogDefaults `yaml:"defaults"`
	Discovery *intentDiscovery       `yaml:"discovery"`
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
