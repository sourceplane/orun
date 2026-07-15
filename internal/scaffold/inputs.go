package scaffold

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Values is a validated, collected input set ready to feed the engine. It
// separates non-secret values (which may appear in provenance hashing and
// render output) from secret values (in-memory only, never written — design
// §8).
type Values struct {
	// Fields is the render model (the template dot). It includes secret
	// fields so a bind:true template can reference them, but the secret sweep
	// (§8) forbids a secret value from surviving into any written file.
	Fields map[string]any
	// secrets records the string values of secret inputs, for the copy-mode
	// secret sweep and to keep them out of the provenance inputs-hash.
	secrets map[string]string
}

// SecretValues returns the collected secret string values (for the copy-mode
// secret sweep, design §8). The map is not persisted anywhere.
func (v Values) SecretValues() []string {
	out := make([]string, 0, len(v.secrets))
	for _, s := range v.secrets {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isSecretField reports whether name was collected as a secret.
func (v Values) isSecretField(name string) bool {
	_, ok := v.secrets[name]
	return ok
}

// nonSecretFields returns the render fields with secret values redacted, for a
// stable, secret-free provenance inputs-hash (design §8, §11).
func (v Values) nonSecretFields() map[string]any {
	out := make(map[string]any, len(v.Fields))
	for k, val := range v.Fields {
		if v.isSecretField(k) {
			out[k] = "<secret>"
			continue
		}
		out[k] = val
	}
	return out
}

// CollectInputs validates raw string-keyed input assignments (from --<input>
// flags or a portal form) against the blueprint's inputs schema, producing a
// typed, validated Values (design §7 collection, §8 secret rule). It does not
// prompt interactively — the caller supplies raw[name] for every provided
// input; a required input with no raw value and no default is an error.
//
// secret fields are held in Values.secrets (in memory only) and never enter the
// provenance inputs-hash (nonSecretFields redacts them).
func CollectInputs(inputs map[string]InputSpec, raw map[string]string) (Values, error) {
	v := Values{
		Fields:  make(map[string]any, len(inputs)),
		secrets: make(map[string]string),
	}

	// Deterministic order for stable error reporting.
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := inputs[name]
		rawVal, provided := raw[name]

		if !provided {
			if spec.Default != nil {
				v.Fields[name] = spec.Default
				if spec.Secret {
					v.secrets[name] = fmt.Sprint(spec.Default)
				}
				continue
			}
			if spec.Required {
				return Values{}, inputErr("input %q is required", name)
			}
			// Optional, no default: populate a typed zero so missingkey=error
			// never fires for a declared input, and `default` can fill it.
			v.Fields[name] = zeroFor(spec.Type)
			if spec.Secret {
				v.secrets[name] = ""
			}
			continue
		}

		typed, err := coerceAndValidate(name, spec, rawVal)
		if err != nil {
			return Values{}, err
		}
		v.Fields[name] = typed
		if spec.Secret {
			v.secrets[name] = rawVal
		}
	}

	// Reject unknown inputs — fail closed on a typo'd --flag.
	for name := range raw {
		if _, ok := inputs[name]; !ok {
			return Values{}, inputErr("unknown input %q (not declared in blueprint inputs)", name)
		}
	}

	return v, nil
}

func coerceAndValidate(name string, spec InputSpec, raw string) (any, error) {
	switch spec.Type {
	case InputString, "":
		if spec.Pattern != "" {
			re, err := regexp.Compile(spec.Pattern)
			if err != nil {
				return nil, inputErr("input %q: invalid pattern %q: %v", name, spec.Pattern, err)
			}
			if !re.MatchString(raw) {
				return nil, inputErr("input %q value %q does not match pattern %q", name, raw, spec.Pattern)
			}
		}
		return raw, nil
	case InputNumber:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, inputErr("input %q must be a number, got %q", name, raw)
		}
		return f, nil
	case InputBoolean:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, inputErr("input %q must be a boolean, got %q", name, raw)
		}
		return b, nil
	case InputEnum:
		for _, allowed := range spec.Values {
			if raw == allowed {
				return raw, nil
			}
		}
		return nil, inputErr("input %q must be one of [%s], got %q", name, strings.Join(spec.Values, ", "), raw)
	case InputObject, InputArray:
		// Structured inputs arrive pre-shaped from a portal form, not a flag
		// string. A raw string for a structured field is a usage error here;
		// the portal path sets Fields directly.
		return nil, inputErr("input %q is a %s and cannot be set from a scalar flag", name, spec.Type)
	default:
		return nil, inputErr("input %q has unknown type %q", name, spec.Type)
	}
}

func zeroFor(t InputType) any {
	switch t {
	case InputNumber:
		return float64(0)
	case InputBoolean:
		return false
	case InputArray:
		return []any{}
	case InputObject:
		return map[string]any{}
	default:
		return ""
	}
}

// inputErr wraps input-validation failures with exit code 1 (design §10).
func inputErr(format string, args ...any) error {
	return &ExitError{Code: 1, Err: fmt.Errorf(format, args...)}
}
