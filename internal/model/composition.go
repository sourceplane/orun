package model

// CompositionConfig declares where composition definitions are loaded from.
type CompositionConfig struct {
	Sources    []CompositionSource   `yaml:"sources,omitempty" json:"sources,omitempty"`
	Resolution CompositionResolution `yaml:"resolution,omitempty" json:"resolution,omitempty"`
}

// CompositionSource describes one source of composition packages.
type CompositionSource struct {
	Name       string            `yaml:"name" json:"name"`
	Kind       string            `yaml:"kind" json:"kind"`
	Path       string            `yaml:"path,omitempty" json:"path,omitempty"`
	Ref        string            `yaml:"ref,omitempty" json:"ref,omitempty"`
	Digest     string            `yaml:"digest,omitempty" json:"digest,omitempty"`
	PullPolicy string            `yaml:"pullPolicy,omitempty" json:"pullPolicy,omitempty"`
	Verify     *VerifyPolicy     `yaml:"verify,omitempty" json:"verify,omitempty"`
	Metadata   map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// VerifyPolicy reserves room for future supply-chain verification settings.
type VerifyPolicy struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	KeyRef   string `yaml:"keyRef,omitempty" json:"keyRef,omitempty"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// CompositionResolution controls how exported composition types are selected.
type CompositionResolution struct {
	Precedence []string          `yaml:"precedence,omitempty" json:"precedence,omitempty"`
	Bindings   map[string]string `yaml:"bindings,omitempty" json:"bindings,omitempty"`
}

// ComponentCompositionRef overrides the source used for a single component.
type ComponentCompositionRef struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
}

// CompositionDocument is the self-describing composition definition.
type CompositionDocument struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       CompositionDocumentSpec `yaml:"spec" json:"spec"`
}

// CompositionDocumentSpec is the portable contract for one composition type.
type CompositionDocumentSpec struct {
	Type        string                 `yaml:"type" json:"type"`
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	DefaultJob  string                 `yaml:"defaultJob" json:"defaultJob"`
	InputSchema map[string]interface{} `yaml:"inputSchema,omitempty" json:"inputSchema,omitempty"`
	Jobs        []JobSpec              `yaml:"jobs" json:"jobs"`
}

// CompositionPackage is the package manifest at the root of a composition package.
type CompositionPackage struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                 `yaml:"kind" json:"kind"`
	Metadata   Metadata               `yaml:"metadata" json:"metadata"`
	Spec       CompositionPackageSpec `yaml:"spec" json:"spec"`
}

// CompositionPackageSpec defines versioned exported compositions.
type CompositionPackageSpec struct {
	Version      string                   `yaml:"version" json:"version"`
	Gluon        CompositionPackageGluon `yaml:"gluon,omitempty" json:"gluon,omitempty"`
	Exports      []CompositionExport      `yaml:"exports" json:"exports"`
	Dependencies []CompositionDependency  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// CompositionPackageGluon constrains compatible gluon versions.
type CompositionPackageGluon struct {
	MinVersion string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"`
}

// CompositionExport maps a logical composition name to a file in the package.
type CompositionExport struct {
	Composition string `yaml:"composition" json:"composition"`
	Path        string `yaml:"path" json:"path"`
}

// CompositionDependency captures optional transitive package references.
type CompositionDependency struct {
	Name     string `yaml:"name" json:"name"`
	Ref      string `yaml:"ref" json:"ref"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// CompositionLock records resolved source digests for reproducible planning.
type CompositionLock struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Sources    []CompositionLockSource `yaml:"sources" json:"sources"`
}

// CompositionLockSource stores one resolved source entry in the lock file.
type CompositionLockSource struct {
	Name           string   `yaml:"name" json:"name"`
	Kind           string   `yaml:"kind" json:"kind"`
	Ref            string   `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path           string   `yaml:"path,omitempty" json:"path,omitempty"`
	ResolvedDigest string   `yaml:"resolvedDigest" json:"resolvedDigest"`
	Exports        []string `yaml:"exports,omitempty" json:"exports,omitempty"`
}