package integrationscli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// Deps are the cmd/orun-side seams the renderer builds against. The renderer
// itself is pure descriptor → cobra construction; auth, tenancy, and the SP5
// secret-authoring implementation stay owned by the caller.
type Deps struct {
	// SecretCreate returns the shipped SP5 `secret create` leaf command for a
	// provider — the byte-identical implementation every secret-authoring verb
	// (derived or served) delegates to. Nil skips secret-authoring verbs.
	SecretCreate func(provider string) *cobra.Command
	// Exec runs one bound, allowlisted invocation (auth + tenancy resolved by
	// the caller at run time, never at tree construction).
	Exec func(cmd *cobra.Command, inv *Invocation) error
	// Debugf logs renderer diagnostics (e.g. a shadowed extension). Nil is a
	// no-op.
	Debugf func(format string, args ...interface{})
}

func (d Deps) debugf(format string, args ...interface{}) {
	if d.Debugf != nil {
		d.Debugf(format, args...)
	}
}

// VerbSpec is one verb of a provider's rendered tree: the (possibly derived)
// declaration plus whether the server served it explicitly.
type VerbSpec struct {
	Verb   configsurface.CliVerb
	Served bool
}

// Standard capability-derived verb construction (design.md §2): a provider
// gets a working namespace by existing. Explicit served entries win over these
// at the same path.
const (
	capSecrets          = "secrets"
	capCredentialBroker = "credential-broker"
	capProvision        = "provision"
)

// secretCreateOps are the allowlisted secret-authoring operations; any verb
// invoking one is mounted as the SP5 delegate so behavior stays byte-identical.
func isSecretCreateOp(op string) bool {
	return op == "config.createBrokeredSecret" || op == "config.createRotatedSecret"
}

// RenderedVerbs merges the standard capability-derived verbs with the
// descriptor's explicitly served verbs. On a path collision the served verb
// wins (server truth). Only live providers render verbs.
func RenderedVerbs(d configsurface.IntegrationDescriptor) []VerbSpec {
	if !d.Live() {
		return nil
	}
	var specs []VerbSpec
	index := map[string]int{}
	add := func(v configsurface.CliVerb, served bool) {
		key := strings.Join(v.Path, " ")
		if i, ok := index[key]; ok {
			if served {
				specs[i] = VerbSpec{Verb: v, Served: true}
			}
			return
		}
		index[key] = len(specs)
		specs = append(specs, VerbSpec{Verb: v, Served: served})
	}

	for _, v := range derivedStandardVerbs(d) {
		add(v, false)
	}
	if d.CLI != nil {
		for _, v := range d.CLI.Verbs {
			if len(v.Path) == 0 {
				continue
			}
			add(v, true)
		}
	}
	return specs
}

// derivedStandardVerbs synthesizes the standard verbs from the descriptor's
// capabilities: connections list|get|revoke + health for every live provider
// (core), templates list + credentials list|revoke for credential-broker,
// secret create for secrets, sandboxes list for provision.
func derivedStandardVerbs(d configsurface.IntegrationDescriptor) []configsurface.CliVerb {
	p := d.Provider
	connArg := configsurface.CliArg{
		Name: "connection", Kind: "positional", Type: "string", Required: true,
		Help: "Integration connection public id (int_…)",
	}
	connFlag := configsurface.CliArg{
		Name: "connection", Kind: "flag", Type: "string", Required: true,
		Help: "Integration connection public id (int_…)",
	}
	verbs := []configsurface.CliVerb{
		{
			Path:    []string{"connections", "list"},
			Summary: "List " + p + " connections for the workspace",
			Invoke:  configsurface.CliInvoke{Plane: "integrations", Op: "integrations.listConnections"},
		},
		{
			Path:    []string{"connections", "get"},
			Summary: "Show one " + p + " connection",
			Args:    []configsurface.CliArg{connArg},
			Invoke: configsurface.CliInvoke{Plane: "integrations", Op: "integrations.getConnection",
				Bind: map[string]string{"connection": "connectionId"}},
		},
		{
			Path:    []string{"connections", "revoke"},
			Summary: "Revoke a " + p + " connection (bound secrets go orphaned)",
			Args:    []configsurface.CliArg{connArg},
			Invoke: configsurface.CliInvoke{Plane: "integrations", Op: "integrations.revokeConnection",
				Bind: map[string]string{"connection": "connectionId"}},
		},
		{
			Path:    []string{"health"},
			Summary: "Show connection health for " + p,
			Invoke:  configsurface.CliInvoke{Plane: "integrations", Op: "integrations.connectionHealth"},
		},
	}
	for _, c := range d.Capabilities {
		switch c {
		case capSecrets:
			verbs = append(verbs, configsurface.CliVerb{
				Path:    []string{"secret", "create"},
				Summary: "Create an integration-bound secret minted from a " + p + " connection",
				Invoke:  configsurface.CliInvoke{Plane: "config", Op: "config.createBrokeredSecret"},
			})
		case capCredentialBroker:
			verbs = append(verbs,
				configsurface.CliVerb{
					Path:    []string{"templates", "list"},
					Summary: "List the scope templates " + p + " can mint against",
					Invoke:  configsurface.CliInvoke{Plane: "integrations", Op: "integrations.listTemplates"},
				},
				configsurface.CliVerb{
					Path:    []string{"credentials", "list"},
					Summary: "List credentials minted from a " + p + " connection (metadata only)",
					Args:    []configsurface.CliArg{connFlag},
					Invoke: configsurface.CliInvoke{Plane: "integrations", Op: "integrations.listMinted",
						Bind: map[string]string{"connection": "connectionId"}},
				},
				configsurface.CliVerb{
					Path:    []string{"credentials", "revoke"},
					Summary: "Revoke a credential minted from a " + p + " connection",
					Args: []configsurface.CliArg{
						{Name: "credential", Kind: "positional", Type: "string", Required: true, Help: "Minted credential id"},
						connFlag,
					},
					Invoke: configsurface.CliInvoke{Plane: "integrations", Op: "integrations.revokeMinted",
						Bind: map[string]string{"credential": "credentialId", "connection": "connectionId"}},
				},
			)
		case capProvision:
			verbs = append(verbs, configsurface.CliVerb{
				Path:    []string{"sandboxes", "list"},
				Summary: "List " + p + " sandboxes provisioned for the workspace",
				Invoke:  configsurface.CliInvoke{Plane: "integrations", Op: "integrations.listSandboxes"},
			})
		}
	}
	return verbs
}

// ProviderCommands builds one cobra subtree per live provider in the registry,
// mounting native extensions after the served/derived verbs (served wins on a
// path collision — RegisterExtension). Dormant/roadmap providers render no
// tree; they appear only in the listing with their status.
func ProviderCommands(registry []configsurface.IntegrationDescriptor, deps Deps) []*cobra.Command {
	var out []*cobra.Command
	for _, d := range registry {
		if cmd := BuildProviderCommand(d, deps); cmd != nil {
			out = append(out, cmd)
		}
	}
	return out
}

// BuildProviderCommand renders one live provider's verb tree. Nil for
// dormant/roadmap providers and blank ids.
func BuildProviderCommand(d configsurface.IntegrationDescriptor, deps Deps) *cobra.Command {
	if !d.Live() || strings.TrimSpace(d.Provider) == "" {
		return nil
	}
	short := d.DisplayName
	if short == "" {
		short = d.Provider
	}
	if d.Tagline != "" {
		short += " — " + d.Tagline
	}
	providerCmd := &cobra.Command{
		Use:   d.Provider,
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return unknownIntegrationVerb(cmd, args[0])
		},
	}
	groups := map[string]*cobra.Command{}
	for _, spec := range RenderedVerbs(d) {
		mountVerb(providerCmd, groups, d.Provider, spec, deps)
	}
	mountExtensions(providerCmd, d.Provider, deps.debugf)
	return providerCmd
}

// mountVerb attaches one verb at its path, creating intermediate group
// commands as needed.
func mountVerb(providerCmd *cobra.Command, groups map[string]*cobra.Command, provider string, spec VerbSpec, deps Deps) {
	v := spec.Verb
	parent := providerCmd
	for i := 0; i < len(v.Path)-1; i++ {
		key := strings.Join(v.Path[:i+1], " ")
		group, ok := groups[key]
		if !ok {
			group = &cobra.Command{
				Use:   v.Path[i],
				Short: "Manage " + provider + " " + v.Path[i],
				Args:  cobra.ArbitraryArgs,
				RunE: func(cmd *cobra.Command, args []string) error {
					if len(args) == 0 {
						return cmd.Help()
					}
					return unknownIntegrationVerb(cmd, args[0])
				},
			}
			groups[key] = group
			parent.AddCommand(group)
		}
		parent = group
	}
	leaf := buildLeafCommand(provider, spec, deps)
	if leaf != nil {
		parent.AddCommand(leaf)
	}
}

// buildLeafCommand builds the runnable command for one verb. Secret-authoring
// verbs delegate to the SP5 implementation (byte-identical, design.md §3);
// everything else binds args and executes through the invoke allowlist. A verb
// naming an op this binary does not compile in still renders (help/completion
// keep working) but running it reports the needs-a-newer-orun error.
func buildLeafCommand(provider string, spec VerbSpec, deps Deps) *cobra.Command {
	v := spec.Verb
	name := v.Path[len(v.Path)-1]
	if isSecretCreateOp(v.Invoke.Op) {
		if deps.SecretCreate == nil {
			return nil
		}
		leaf := deps.SecretCreate(provider)
		// The delegate ships as `create <KEY>`; re-home its name onto the
		// served path's leaf segment without touching its behavior. Only an
		// explicitly SERVED summary overrides the delegate's own help — the
		// derived standard verb keeps the SP5 wording byte-identical.
		if rest := strings.TrimPrefix(leaf.Use, leaf.Name()); leaf.Name() != name {
			leaf.Use = name + rest
		}
		if spec.Served && v.Summary != "" {
			leaf.Short = v.Summary
		}
		return leaf
	}

	leaf := &cobra.Command{
		Use:   verbUseLine(name, v),
		Short: v.Summary,
		Args:  positionalValidator(v),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !OpKnown(v.Invoke.Op) {
				return unknownOpError(provider, v)
			}
			inv, err := BindArgs(provider, v, cmd, args)
			if err != nil {
				return err
			}
			if deps.Exec == nil {
				return fmt.Errorf("orun integrations %s %s: no executor wired", provider, strings.Join(v.Path, " "))
			}
			return deps.Exec(cmd, inv)
		},
	}
	addVerbFlags(leaf, v)
	return leaf
}

// unknownOpError is the forward-compatibility error for a served verb whose
// operation is newer than this binary's allowlist: render, never crash, never
// guess (design.md §1).
func unknownOpError(provider string, v configsurface.CliVerb) error {
	return fmt.Errorf("`orun integrations %s %s` needs a newer orun: this build does not support the served operation %q\n\nupgrade orun, then re-run (or `orun integrations sync` to refresh the verb tree)",
		provider, strings.Join(v.Path, " "), v.Invoke.Op)
}

// verbUseLine renders the cobra Use line: leaf name plus uppercase positional
// placeholders.
func verbUseLine(name string, v configsurface.CliVerb) string {
	use := name
	for _, a := range v.Args {
		if a.Kind != "positional" {
			continue
		}
		ph := "<" + strings.ToUpper(a.Name) + ">"
		if a.Repeat {
			ph += "..."
		}
		if !a.Required {
			ph = "[" + ph + "]"
		}
		use += " " + ph
	}
	return use
}

// positionalValidator checks positional arity against the verb's declaration,
// failing with the missing/unexpected-argument dialect the static SP5 grammar
// already speaks.
func positionalValidator(v configsurface.CliVerb) cobra.PositionalArgs {
	var pos []configsurface.CliArg
	repeat := false
	required := 0
	for _, a := range v.Args {
		if a.Kind != "positional" {
			continue
		}
		pos = append(pos, a)
		if a.Repeat {
			repeat = true
		}
		if a.Required {
			required++
		}
	}
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < required {
			return fmt.Errorf("missing <%s>\n\nusage:\n  %s", strings.ToUpper(pos[len(args)].Name), cmd.UseLine())
		}
		if !repeat && len(args) > len(pos) {
			return fmt.Errorf("unexpected argument %q\n\nusage:\n  %s", args[len(pos)], cmd.UseLine())
		}
		return nil
	}
}

// addVerbFlags registers the verb's declared flags plus the uniform-output
// flags: --json everywhere, --yes on mutating (confirm-gated) ops.
func addVerbFlags(cmd *cobra.Command, v configsurface.CliVerb) {
	for _, a := range v.Args {
		if a.Kind != "flag" {
			continue
		}
		switch a.Type {
		case "int":
			cmd.Flags().Int(a.Name, 0, a.Help)
		case "bool":
			cmd.Flags().Bool(a.Name, false, a.Help)
		case "kv":
			cmd.Flags().StringArray(a.Name, nil, kvHelp(a.Help))
		default: // string | enum
			if a.Repeat {
				cmd.Flags().StringArray(a.Name, nil, a.Help)
			} else {
				cmd.Flags().String(a.Name, "", enumHelp(a))
			}
		}
		if a.Required {
			_ = cmd.MarkFlagRequired(a.Name)
		}
	}
	if cmd.Flags().Lookup("json") == nil {
		cmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human table")
	}
	if opConfirms(v.Invoke.Op) && cmd.Flags().Lookup("yes") == nil {
		cmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	}
}

func kvHelp(help string) string {
	if help == "" {
		return "key=value (repeatable)"
	}
	return help + " (key=value, repeatable)"
}

func enumHelp(a configsurface.CliArg) string {
	if a.Type == "enum" && len(a.Enum) > 0 {
		suffix := "one of: " + strings.Join(a.Enum, ", ")
		if a.Help == "" {
			return suffix
		}
		return a.Help + " (" + suffix + ")"
	}
	return a.Help
}

// unknownIntegrationVerb turns a typo'd word under a rendered provider tree
// into an actionable "did you mean" error with a non-zero exit — the
// unknownSecretsSubcommand dialect generalized over the rendered tree.
func unknownIntegrationVerb(parent *cobra.Command, name string) error {
	var canonical, candidates []string
	for _, c := range parent.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		canonical = append(canonical, c.Name())
		candidates = append(candidates, c.Name())
		candidates = append(candidates, c.Aliases...)
	}
	sort.Strings(canonical)
	sort.Strings(candidates)
	var b strings.Builder
	fmt.Fprintf(&b, "unknown subcommand %q for %q", name, parent.CommandPath())
	if suggestion := ui.SuggestMatch(name, candidates); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  %s %s", parent.CommandPath(), suggestion)
	}
	if len(canonical) > 0 {
		b.WriteString("\n\navailable subcommands:\n")
		for _, n := range canonical {
			fmt.Fprintf(&b, "  %s\n", n)
		}
	}
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// RenderProviderListing renders the `orun integrations` provider table:
// provider id, display name, category, connected count (when projected), and
// status — every provider in the registry, including dormant/roadmap rows.
func RenderProviderListing(registry []configsurface.IntegrationDescriptor) string {
	sorted := make([]configsurface.IntegrationDescriptor, len(registry))
	copy(sorted, registry)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Provider < sorted[j].Provider })

	rows := make([][]string, 0, len(sorted))
	for _, d := range sorted {
		connected := "-"
		if d.Connected > 0 {
			connected = strconv.Itoa(d.Connected)
		}
		rows = append(rows, []string{
			d.Provider,
			orDash(d.DisplayName),
			orDash(d.Category),
			connected,
			defaultString(d.Status, "live"),
		})
	}
	return renderColumns([]string{"PROVIDER", "NAME", "CATEGORY", "CONNECTED", "STATUS"}, rows)
}

// RegistryStats counts the providers and rendered verbs in a registry — the
// `orun integrations sync` summary numbers.
func RegistryStats(registry []configsurface.IntegrationDescriptor) (providers, verbs int) {
	providers = len(registry)
	for _, d := range registry {
		verbs += len(RenderedVerbs(d))
	}
	return providers, verbs
}
