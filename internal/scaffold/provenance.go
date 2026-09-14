package scaffold

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	yaml "gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/objectstore"
)

// ProvenanceRelPath is where the lock is written under the output root.
const ProvenanceRelPath = ".orun/provenance.lock"

// ProvenanceAPIVersion / Kind envelope the lock document.
const (
	ProvenanceAPIVersion = "orun.dev/v1"
	ProvenanceKind       = "ScaffoldProvenance"
)

// Provenance is the .orun/provenance.lock content (design §11). It records the
// Merkle-pinned blueprint + source digests, a secret-free inputs hash, and the
// per-module mode/target — enough to re-render (`orun scaffold upgrade`) and to
// prove lineage even for a single scaffolded component.
type Provenance struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Blueprint  ProvBlueprint  `yaml:"blueprint" json:"blueprint"`
	Sources    []ProvSource   `yaml:"sources,omitempty" json:"sources,omitempty"`
	InputsHash string         `yaml:"inputsHash" json:"inputsHash"`
	Inputs     map[string]any `yaml:"inputs" json:"inputs"`
	Modules    []ProvModule   `yaml:"modules" json:"modules"`
	Consumed   []ProvConsumed `yaml:"consumed,omitempty" json:"consumed,omitempty"`
}

// ProvBlueprint pins the blueprint document by digest.
type ProvBlueprint struct {
	Name   string `yaml:"name" json:"name"`
	Digest string `yaml:"digest" json:"digest"`
}

// ProvSource pins one resolved source by digest.
type ProvSource struct {
	Name   string `yaml:"name" json:"name"`
	Kind   string `yaml:"kind" json:"kind"`
	Digest string `yaml:"digest" json:"digest"`
}

// ProvModule records how a module was placed.
type ProvModule struct {
	Name    string   `yaml:"name" json:"name"`
	Mode    string   `yaml:"mode" json:"mode"`
	Targets []string `yaml:"targets,omitempty" json:"targets,omitempty"`
}

// ProvConsumed records a consume-mode dependency.
type ProvConsumed struct {
	Module string `yaml:"module" json:"module"`
	Source string `yaml:"source" json:"source"`
	From   string `yaml:"from" json:"from"`
	Digest string `yaml:"digest,omitempty" json:"digest,omitempty"`
}

func buildProvenance(ctx context.Context, store objectstore.ObjectStore, rawBlueprint []byte, bp *Blueprint, values Values, sources map[string]ResolvedSource, placed map[string]PlacedFile, consumed []ConsumedDep) (Provenance, error) {
	bpDigest, err := store.PutBlob(ctx, rawBlueprint)
	if err != nil {
		return Provenance{}, err
	}

	// Per-module targets, from the placed set.
	targetsByModule := map[string][]string{}
	for path, f := range placed {
		targetsByModule[f.Module] = append(targetsByModule[f.Module], path)
	}
	modules := make([]ProvModule, 0, len(bp.Modules))
	for _, m := range bp.Modules {
		t := targetsByModule[m.Name]
		sort.Strings(t)
		modules = append(modules, ProvModule{Name: m.Name, Mode: string(m.Mode), Targets: t})
	}

	provSources := make([]ProvSource, 0, len(sources))
	for _, s := range bp.Sources {
		if rs, ok := sources[s.Name]; ok {
			provSources = append(provSources, ProvSource{Name: rs.Name, Kind: string(rs.Kind), Digest: string(rs.Digest)})
		}
	}
	sort.Slice(provSources, func(i, j int) bool { return provSources[i].Name < provSources[j].Name })

	provConsumed := make([]ProvConsumed, 0, len(consumed))
	for _, c := range consumed {
		provConsumed = append(provConsumed, ProvConsumed{Module: c.Module, Source: c.Source, From: c.From, Digest: c.Digest})
	}

	ns := values.nonSecretFields()
	hash, err := inputsHash(ns)
	if err != nil {
		return Provenance{}, err
	}

	return Provenance{
		APIVersion: ProvenanceAPIVersion,
		Kind:       ProvenanceKind,
		Blueprint:  ProvBlueprint{Name: bp.Metadata.Name, Digest: string(bpDigest)},
		Sources:    provSources,
		InputsHash: hash,
		Inputs:     ns,
		Modules:    modules,
		Consumed:   provConsumed,
	}, nil
}

// inputsHash is a stable sha256 over the secret-free inputs (design §8/§11):
// secrets are already redacted by nonSecretFields, so the hash never depends on
// a secret value.
func inputsHash(fields map[string]any) (string, error) {
	// Canonical JSON: json.Marshal sorts map keys, giving a stable encoding.
	data, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

func writeProvenance(outDir string, prov Provenance) error {
	data, err := yaml.Marshal(prov)
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, filepath.FromSlash(ProvenanceRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadProvenance reads and decodes an existing lock from a scaffolded tree.
func ReadProvenance(outDir string) (Provenance, error) {
	path := filepath.Join(outDir, filepath.FromSlash(ProvenanceRelPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return Provenance{}, err
	}
	var prov Provenance
	if err := yaml.Unmarshal(data, &prov); err != nil {
		return Provenance{}, fmt.Errorf("parse provenance.lock: %w", err)
	}
	return prov, nil
}
