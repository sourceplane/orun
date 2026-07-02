package catalogext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/model"
)

// SecretsExtensionKey is the catalog extension key for the secrets facet
// (specs/orun-secrets/platform-integration.md §1, SD-14).
const SecretsExtensionKey = "x-orun-secrets"

// SecretRequirement is one declared, offline-computable secret a component
// needs: the logical Key, the composition Profile that declared it, and whether
// it is Required. Value-free — Invariant 1 applies to the catalog too.
type SecretRequirement struct {
	Key      string `json:"key"`
	Profile  string `json:"profile"`
	Required bool   `json:"required"`
}

// SecretsFacet is the x-orun-secrets extension block. Only the static half
// (requirements) is derived here; the live keys (bindings/rotation/syncs from
// backend metadata) are merged in by a later slice (SM4, orun-cloud). The shape
// leaves room for them without a schema change.
type SecretsFacet struct {
	Requirements []SecretRequirement `json:"requirements,omitempty"`
}

// DeriveComponentSecretRequirements computes the union of every profile's
// secretBindings across the given execution profiles — the declared, offline
// half of the facet (platform-integration.md §1). Entries are deterministically
// ordered (profile, then key) and deduplicated. Returns nil when no profile
// declares a binding.
func DeriveComponentSecretRequirements(profiles map[string]model.ExecutionProfile) []SecretRequirement {
	if len(profiles) == 0 {
		return nil
	}
	profileNames := make([]string, 0, len(profiles))
	for name := range profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)

	seen := make(map[SecretRequirement]struct{})
	out := make([]SecretRequirement, 0)
	for _, profileName := range profileNames {
		profile := profiles[profileName]
		keys := make([]string, 0, len(profile.SecretBindings))
		for k := range profile.SecretBindings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			req := SecretRequirement{
				Key:      key,
				Profile:  profileName,
				Required: profile.SecretBindings[key].Required,
			}
			if _, dup := seen[req]; dup {
				continue
			}
			seen[req] = struct{}{}
			out = append(out, req)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AttachSecretsFacet merges the derived requirements into an extensions map as
// the x-orun-secrets block, preserving any live keys already present so a later
// live-plane pass can co-exist. Returns the (possibly newly allocated) map; a
// nil facet with an empty map yields nil so a binding-less component leaves the
// catalog manifest hash unchanged. The block is stored as SecretsFacet so it
// round-trips through the validator.
func AttachSecretsFacet(extensions map[string]any, reqs []SecretRequirement) map[string]any {
	if len(reqs) == 0 {
		return extensions
	}
	if extensions == nil {
		extensions = make(map[string]any)
	}
	extensions[SecretsExtensionKey] = SecretsFacet{Requirements: reqs}
	return extensions
}

// strictSecretsFacet is the closed schema the validator round-trips through.
// DisallowUnknownFields makes any extra field — notably a value/ciphertext leak —
// a validation error, structurally enforcing Invariant 1 on the catalog.
type strictSecretsFacet struct {
	Requirements []strictRequirement `json:"requirements"`
	Bindings     []strictBinding     `json:"bindings"`
	Rotation     []strictRotation    `json:"rotation"`
	Syncs        []strictSync        `json:"syncs"`
}

type strictRequirement struct {
	Key      string `json:"key"`
	Profile  string `json:"profile"`
	Required bool   `json:"required"`
}

// The live-plane entry shapes (platform-integration.md §1). Declared here so a
// well-formed live block validates; all fields are value-free metadata.
type strictBinding struct {
	Key        string `json:"key"`
	Env        string `json:"env"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
	ServesFrom string `json:"servesFrom"`
}

type strictRotation struct {
	Key       string `json:"key"`
	Env       string `json:"env"`
	RotatedAt string `json:"rotatedAt"`
	AgeDays   int    `json:"ageDays"`
}

type strictSync struct {
	Key       string `json:"key"`
	Env       string `json:"env"`
	Target    string `json:"target"`
	EntityRef string `json:"entityRef"`
	Version   int    `json:"version"`
	Status    string `json:"status"`
}

// ValidateSecretsFacet validates an x-orun-secrets block: the shape
// {requirements[], bindings[], rotation[], syncs[]} (all optional), each entry
// value-free. Any unknown/value-shaped field is rejected
// (platform-integration.md §1). Registered via RegisterStandard.
func ValidateSecretsFacet(block any) error {
	raw, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("x-orun-secrets block is not encodable: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var facet strictSecretsFacet
	if err := dec.Decode(&facet); err != nil {
		return fmt.Errorf("x-orun-secrets block malformed (a value or unknown field is not permitted): %w", err)
	}
	return nil
}

// RegisterStandard registers the built-in extension validators (currently the
// secrets facet) on r. It is the canonical registration home for orun's own
// x-* blocks — call it when constructing a registry for catalog validation.
func RegisterStandard(r *Registry) {
	if r == nil {
		return
	}
	r.Register(SecretsExtensionKey, ValidateSecretsFacet)
}
