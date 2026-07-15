package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	yaml "gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/scaffold"
)

var (
	scaffoldBlueprint  string
	scaffoldValuesFile string
	scaffoldOut        string
	scaffoldSet        []string
	scaffoldRunHooks   bool

	upgradeBlueprint string
	upgradeOut       string
	upgradeApply     bool
)

// scaffoldNewCmd is the unified scaffolding entrypoint (design §3). `orun new`,
// `orun create`, and `orun instantiate` all front the same pipeline — the
// single-component and full-repo scales are one operation.
var scaffoldNewCmd = &cobra.Command{
	Use:     "new",
	Aliases: []string{"create", "instantiate"},
	Short:   "Scaffold a component or instantiate a repo from a Blueprint",
	Long: `Scaffold a component or instantiate a whole repo from a kind: Blueprint.

One engine, one language, two scales: a one-module blueprint with no sources is
the single-service scaffolder; the same schema with a source and many modules is
the product instantiator. Every generated component.yaml must pass both the
plan-engine and catalog parsers before anything is written (fail closed).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScaffoldNew(cmd.Context())
	},
}

// scaffoldUpgradeCmd re-renders a newer blueprint against a scaffolded tree's
// provenance.lock and 3-way-merges (design §11).
var scaffoldUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Re-render a newer Blueprint into a scaffolded tree (3-way merge)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScaffoldUpgrade(cmd.Context())
	},
}

func registerNewCommand(root *cobra.Command) {
	root.AddCommand(scaffoldNewCmd)
	scaffoldNewCmd.AddCommand(scaffoldUpgradeCmd)

	scaffoldNewCmd.Flags().StringVar(&scaffoldBlueprint, "blueprint", "", "Path to the Blueprint document (required)")
	scaffoldNewCmd.Flags().StringVar(&scaffoldValuesFile, "values", "", "Path to a YAML values file feeding blueprint inputs")
	scaffoldNewCmd.Flags().StringVar(&scaffoldOut, "out", ".", "Output directory (created if absent)")
	scaffoldNewCmd.Flags().StringArrayVar(&scaffoldSet, "set", nil, "Set an input as key=value (repeatable; overrides --values)")
	scaffoldNewCmd.Flags().BoolVar(&scaffoldRunHooks, "run-hooks", false, "Execute declared postInstantiate hooks (outside the sandbox)")
	_ = scaffoldNewCmd.MarkFlagRequired("blueprint")

	scaffoldUpgradeCmd.Flags().StringVar(&upgradeBlueprint, "blueprint", "", "Path to the newer Blueprint (defaults to the one pinned in the lock)")
	scaffoldUpgradeCmd.Flags().StringVar(&upgradeOut, "out", ".", "Target scaffolded directory (holds .orun/provenance.lock)")
	scaffoldUpgradeCmd.Flags().BoolVar(&upgradeApply, "apply", false, "Apply non-conflicting updates (default: dry-run report)")
}

func runScaffoldNew(ctx context.Context) error {
	bpBytes, err := os.ReadFile(scaffoldBlueprint)
	if err != nil {
		return exitErr(6, "read blueprint: %v", err)
	}
	inputs, err := collectScaffoldInputs()
	if err != nil {
		return err
	}
	// Parse the blueprint up front so we can prompt interactively for any
	// declared input the flags/values did not supply (design §7 collection).
	bp, err := scaffold.ParseBlueprint(bpBytes)
	if err != nil {
		return exitErr(6, "%v", err)
	}
	if err := promptMissingInputs(bp.Inputs, inputs); err != nil {
		return err
	}
	store, err := openScaffoldStore(scaffoldOut)
	if err != nil {
		return err
	}

	res, err := scaffold.Run(ctx, scaffold.Options{
		Blueprint:     bpBytes,
		Inputs:        inputs,
		OutDir:        scaffoldOut,
		Store:         store,
		RunHooks:      scaffoldRunHooks,
		SourceBaseDir: filepath.Dir(scaffoldBlueprint),
	})
	if err != nil {
		return err // *scaffold.ExitError carries the exit code
	}

	fmt.Printf("✓ scaffolded %d file(s) into %s\n", len(res.Files), scaffoldOut)
	phased := len(res.Phases) > 1 || (len(res.Phases) == 1 && res.Phases[0].Name != "")
	for _, phase := range res.Phases {
		if phased {
			label := phase.Name
			if len(phase.Hooks) > 0 {
				hookIDs := make([]string, len(phase.Hooks))
				for i, h := range phase.Hooks {
					hookIDs[i] = h.ID
				}
				label = fmt.Sprintf("%s (hooks: %s)", phase.Name, strings.Join(hookIDs, ", "))
			}
			fmt.Printf("  ▸ phase: %s\n", label)
		}
		for _, batch := range phase.Batches {
			indent := "  "
			if phased {
				indent = "    "
			}
			fmt.Printf("%s· batch: %s\n", indent, strings.Join(batch, ", "))
		}
	}
	for _, f := range res.Files {
		fmt.Printf("    %s\n", f)
	}
	if len(res.Consumed) > 0 {
		fmt.Printf("  consumed (pinned deps, no bytes):\n")
		for _, c := range res.Consumed {
			fmt.Printf("    %s (source %s, from %s)\n", c.Module, c.Source, c.From)
		}
	}
	fmt.Printf("  provenance: %s (inputs %s)\n", filepath.Join(scaffoldOut, scaffold.ProvenanceRelPath), res.Provenance.InputsHash)

	// Repo-scale gate (design §10): if the scaffolded tree is an orun workspace
	// (has an intent), additionally run validate + plan --dry-run before
	// declaring success. Component-scale scaffolds skip this — their gate is
	// the both-parsers + resolve check already run inside scaffold.Run.
	if intent := findScaffoldedIntent(scaffoldOut); intent != "" {
		if err := repoScaleGate(intent); err != nil {
			return exitErr(1, "repo-scale gate failed: %v", err)
		}
		fmt.Printf("  repo gate: validate + plan --dry-run passed\n")
	}
	return nil
}

func runScaffoldUpgrade(ctx context.Context) error {
	store, err := openScaffoldStore(upgradeOut)
	if err != nil {
		return err
	}
	var newBP []byte
	if upgradeBlueprint != "" {
		newBP, err = os.ReadFile(upgradeBlueprint)
		if err != nil {
			return exitErr(6, "read blueprint: %v", err)
		}
	}
	baseDir := ""
	if upgradeBlueprint != "" {
		baseDir = filepath.Dir(upgradeBlueprint)
	}
	res, err := scaffold.Upgrade(ctx, scaffold.UpgradeOptions{
		TargetDir:     upgradeOut,
		NewBlueprint:  newBP,
		Store:         store,
		Apply:         upgradeApply,
		SourceBaseDir: baseDir,
	})
	if err != nil {
		return err
	}
	conflicts := 0
	for _, m := range res.Merges {
		fmt.Printf("  %-9s %s\n", m.Status, m.Path)
		if m.Status == scaffold.MergeConflict {
			conflicts++
		}
	}
	if res.Applied {
		fmt.Printf("✓ applied non-conflicting updates to %s\n", upgradeOut)
	} else {
		fmt.Printf("dry-run (pass --apply to write updates)\n")
	}
	if conflicts > 0 {
		return exitErr(1, "%d file(s) conflict — human edits preserved, resolve manually", conflicts)
	}
	return nil
}

// collectScaffoldInputs merges a --values YAML file (scalars flattened to
// strings) with repeatable --set key=value overrides.
func collectScaffoldInputs() (map[string]string, error) {
	inputs := map[string]string{}
	if scaffoldValuesFile != "" {
		data, err := os.ReadFile(scaffoldValuesFile)
		if err != nil {
			return nil, exitErr(6, "read values: %v", err)
		}
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, exitErr(1, "parse values: %v", err)
		}
		for k, v := range raw {
			inputs[k] = fmt.Sprint(v)
		}
	}
	for _, kv := range scaffoldSet {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, exitErr(1, "invalid --set %q (want key=value)", kv)
		}
		inputs[parts[0]] = parts[1]
	}
	return inputs, nil
}

// promptMissingInputs fills in any declared input the flags/values did not
// supply, by asking on an interactive terminal (design §7 collection). Secret
// inputs are read without echo and never displayed. On a non-interactive stdin
// (CI/piped) it prompts for nothing and lets scaffold.Run fail closed on a
// missing required input, so automation stays deterministic. The `inputs` map
// is mutated in place.
func promptMissingInputs(specs map[string]scaffold.InputSpec, inputs map[string]string) error {
	// Only prompt when both stdin and stdout are terminals.
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	// Deterministic order: required-without-default first, then the rest,
	// alphabetical within each group.
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri := specs[names[i]].Required && specs[names[i]].Default == nil
		rj := specs[names[j]].Required && specs[names[j]].Default == nil
		if ri != rj {
			return ri
		}
		return names[i] < names[j]
	})

	reader := bufio.NewReader(os.Stdin)
	for _, name := range names {
		if _, ok := inputs[name]; ok {
			continue // supplied via flag/values
		}
		spec := specs[name]
		val, err := promptOneInput(reader, name, spec)
		if err != nil {
			return exitErr(1, "%v", err)
		}
		if val != "" {
			inputs[name] = val
		}
		// Empty answer: leave unset so the default (or required-error) applies.
	}
	return nil
}

func promptOneInput(reader *bufio.Reader, name string, spec scaffold.InputSpec) (string, error) {
	label := name
	if spec.Description != "" {
		label = fmt.Sprintf("%s — %s", name, spec.Description)
	}
	hint := ""
	switch {
	case spec.Type == scaffold.InputEnum && len(spec.Values) > 0:
		hint = " [" + strings.Join(spec.Values, "|") + "]"
	case spec.Default != nil && spec.Default != "":
		hint = fmt.Sprintf(" (default: %v)", spec.Default)
	case !spec.Required:
		hint = " (optional)"
	}

	if spec.Secret {
		fmt.Printf("%s%s (input hidden): ", label, hint)
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("reading %q: %w", name, err)
		}
		return string(data), nil
	}

	fmt.Printf("%s%s: ", label, hint)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading %q: %w", name, err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// openScaffoldStore opens (or creates) a local object store under the output
// tree's .orun, so the scaffolded repo carries its own pinned sources +
// blueprint — the lineage that makes `upgrade` possible (design §11).
func openScaffoldStore(outDir string) (*objectstore.LocalStore, error) {
	abs, err := filepath.Abs(filepath.Join(outDir, ".orun"))
	if err != nil {
		return nil, exitErr(1, "resolve store root: %v", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, exitErr(1, "create store root: %v", err)
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: objectModelRoot(abs)})
	if err != nil {
		return nil, exitErr(1, "open object store: %v", err)
	}
	return store, nil
}

// findScaffoldedIntent returns the path to an orun intent file inside a
// scaffolded tree, or "" if the scaffold is a component (no intent).
func findScaffoldedIntent(outDir string) string {
	for _, name := range []string{"intent.yaml", "intent.yml"} {
		p := filepath.Join(outDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// repoScaleGate runs orun validate + plan --dry-run against a scaffolded
// workspace by driving the existing in-process pipelines with the scaffolded
// intent (design §10). It restores the global intent path afterward.
func repoScaleGate(intentPath string) error {
	savedIntent := intentFile
	savedOut, savedFmt := outputFile, outputFormat
	defer func() {
		intentFile = savedIntent
		outputFile, outputFormat = savedOut, savedFmt
	}()
	intentFile = intentPath
	if err := validateFiles(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	// plan --dry-run: compile the whole DAG offline without writing a plan file.
	outputFile = ""
	if err := generatePlan(); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	return nil
}
