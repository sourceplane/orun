package integrationscli

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/spf13/cobra"
)

// ArgValue is one parsed argument value, typed per the descriptor's CliArg.
type ArgValue struct {
	Kind string // string | int | bool | list | kv
	Str  string
	Int  int
	Bool bool
	List []string
	KV   map[string]string
	// Set reports whether the caller supplied the value (vs. an absent
	// optional).
	Set bool
}

// Invocation is one bound verb execution: the provider, the verb's declared
// invoke (op + bind map), and the parsed arg values. It carries no auth, no
// URL, and never a secret value — execution maps it onto a typed plane call
// through the compiled-in allowlist.
type Invocation struct {
	Provider string
	Path     []string
	Op       string
	Bind     map[string]string
	Values   map[string]ArgValue
	JSON     bool
	Yes      bool
}

// FieldString resolves the string value bound to a request field: first via
// the verb's bind map (arg name → field), then by an arg literally named after
// the field, then by the conventional short name (connectionId → connection).
func (inv *Invocation) FieldString(field string) string {
	for argName, target := range inv.Bind {
		if target == field {
			if v, ok := inv.Values[argName]; ok && v.Set {
				return v.Str
			}
		}
	}
	if v, ok := inv.Values[field]; ok && v.Set {
		return v.Str
	}
	if short := strings.TrimSuffix(field, "Id"); short != field {
		if v, ok := inv.Values[short]; ok && v.Set {
			return v.Str
		}
	}
	return ""
}

// BindArgs parses one verb invocation's positionals and flags into a typed
// Invocation, validating enum membership and kv shape. Purely syntactic — the
// server revalidates every execution.
func BindArgs(provider string, v configsurface.CliVerb, cmd *cobra.Command, args []string) (*Invocation, error) {
	inv := &Invocation{
		Provider: provider,
		Path:     v.Path,
		Op:       v.Invoke.Op,
		Bind:     v.Invoke.Bind,
		Values:   map[string]ArgValue{},
	}
	posIdx := 0
	for _, a := range v.Args {
		var val ArgValue
		var err error
		if a.Kind == "positional" {
			val, posIdx, err = bindPositional(a, args, posIdx)
		} else {
			val, err = bindFlag(a, cmd)
		}
		if err != nil {
			return nil, err
		}
		inv.Values[a.Name] = val
	}
	if f := cmd.Flags().Lookup("json"); f != nil {
		inv.JSON, _ = cmd.Flags().GetBool("json")
	}
	if f := cmd.Flags().Lookup("yes"); f != nil {
		inv.Yes, _ = cmd.Flags().GetBool("yes")
	}
	return inv, nil
}

func bindPositional(a configsurface.CliArg, args []string, idx int) (ArgValue, int, error) {
	if a.Repeat {
		rest := args[idx:]
		for _, raw := range rest {
			if err := validateEnum(a, raw); err != nil {
				return ArgValue{}, idx, err
			}
		}
		return ArgValue{Kind: "list", List: rest, Set: len(rest) > 0}, len(args), nil
	}
	if idx >= len(args) {
		if a.Required {
			return ArgValue{}, idx, fmt.Errorf("missing <%s>", strings.ToUpper(a.Name))
		}
		return ArgValue{Kind: "string"}, idx, nil
	}
	raw := args[idx]
	if err := validateEnum(a, raw); err != nil {
		return ArgValue{}, idx, err
	}
	return ArgValue{Kind: "string", Str: raw, Set: true}, idx + 1, nil
}

func bindFlag(a configsurface.CliArg, cmd *cobra.Command) (ArgValue, error) {
	flags := cmd.Flags()
	if flags.Lookup(a.Name) == nil {
		return ArgValue{}, fmt.Errorf("verb declares flag --%s but it was not registered", a.Name)
	}
	set := flags.Changed(a.Name)
	switch a.Type {
	case "int":
		n, err := flags.GetInt(a.Name)
		if err != nil {
			return ArgValue{}, err
		}
		return ArgValue{Kind: "int", Int: n, Set: set}, nil
	case "bool":
		b, err := flags.GetBool(a.Name)
		if err != nil {
			return ArgValue{}, err
		}
		return ArgValue{Kind: "bool", Bool: b, Set: set}, nil
	case "kv":
		raw, err := flags.GetStringArray(a.Name)
		if err != nil {
			return ArgValue{}, err
		}
		kv, err := parseKVPairs(a.Name, raw)
		if err != nil {
			return ArgValue{}, err
		}
		return ArgValue{Kind: "kv", KV: kv, Set: set}, nil
	default: // string | enum
		if a.Repeat {
			list, err := flags.GetStringArray(a.Name)
			if err != nil {
				return ArgValue{}, err
			}
			for _, raw := range list {
				if err := validateEnum(a, raw); err != nil {
					return ArgValue{}, err
				}
			}
			return ArgValue{Kind: "list", List: list, Set: set}, nil
		}
		s, err := flags.GetString(a.Name)
		if err != nil {
			return ArgValue{}, err
		}
		if set {
			if err := validateEnum(a, s); err != nil {
				return ArgValue{}, err
			}
		}
		return ArgValue{Kind: "string", Str: s, Set: set}, nil
	}
}

// validateEnum rejects a value outside the arg's declared enum, listing what
// IS declared (the capability-read error dialect).
func validateEnum(a configsurface.CliArg, raw string) error {
	if a.Type != "enum" || len(a.Enum) == 0 {
		return nil
	}
	for _, e := range a.Enum {
		if e == raw {
			return nil
		}
	}
	label := a.Name
	if a.Kind == "flag" {
		label = "--" + a.Name
	}
	return fmt.Errorf("%s must be one of: %s (got %q)", label, strings.Join(a.Enum, ", "), raw)
}

// parseKVPairs parses repeatable key=value strings into the wire map,
// mirroring the SP5 --param dialect (split on the first '=', duplicates
// rejected).
func parseKVPairs(name string, raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		i := strings.Index(kv, "=")
		if i <= 0 || strings.TrimSpace(kv[:i]) == "" {
			return nil, fmt.Errorf("--%s must be key=value, got %q", name, kv)
		}
		key := strings.TrimSpace(kv[:i])
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("--%s %q supplied more than once", name, key)
		}
		out[key] = kv[i+1:]
	}
	return out, nil
}
