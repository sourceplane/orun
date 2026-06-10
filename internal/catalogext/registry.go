// Package catalogext is the typed extension registry for the software catalog
// (orun-service-catalog SC6, data-model.md §8). The catalog envelope carries an
// `extensions` block of namespaced `x-<vendor>` entries; this registry lets a
// vendor register a validator for its block without a core schema bump.
//
// Three guarantees (data-model.md §8):
//  1. a registered extension is validated against its schema;
//  2. an unknown `x-*` block is preserved verbatim (never dropped) — guaranteed
//     structurally by the envelope carrying extensions as a generic map, so the
//     registry never mutates the block;
//  3. validation is non-fatal per block — a registered-but-invalid block yields
//     an error the caller surfaces, it does not erase the data.
package catalogext

import (
	"fmt"
	"sort"
	"strings"
)

// ExtensionPrefix is the required namespace prefix for an extension key.
const ExtensionPrefix = "x-"

// Validator checks one extension block's value. It returns a non-nil error when
// the block is malformed for its registered schema.
type Validator func(block any) error

// Registry maps an extension key (e.g. "x-datadog") to its validator.
type Registry struct {
	validators map[string]Validator
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{validators: map[string]Validator{}} }

// Register associates a validator with an extension key. The key must carry the
// x- prefix; Register panics on a non-namespaced key (a programming error, not
// runtime input).
func (r *Registry) Register(key string, v Validator) {
	if !strings.HasPrefix(key, ExtensionPrefix) {
		panic(fmt.Sprintf("catalogext: extension key %q must start with %q", key, ExtensionPrefix))
	}
	r.validators[key] = v
}

// Known reports whether key has a registered validator.
func (r *Registry) Known(key string) bool {
	_, ok := r.validators[key]
	return ok
}

// Validate checks every registered extension present in the block set and
// returns the collected errors (deterministically ordered by key). Unknown x-*
// blocks are left untouched — preserved, never validated away. A non-namespaced
// key is reported as an error (extensions must be namespaced, §8).
func (r *Registry) Validate(extensions map[string]any) []error {
	if len(extensions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(extensions))
	for k := range extensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs []error
	for _, k := range keys {
		if !strings.HasPrefix(k, ExtensionPrefix) {
			errs = append(errs, fmt.Errorf("catalogext: extension key %q is not namespaced (must start with %q)", k, ExtensionPrefix))
			continue
		}
		if v, ok := r.validators[k]; ok {
			if err := v(extensions[k]); err != nil {
				errs = append(errs, fmt.Errorf("catalogext: extension %q invalid: %w", k, err))
			}
		}
		// Unknown but namespaced: preserved, not an error.
	}
	return errs
}
