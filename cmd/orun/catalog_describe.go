package main

// catalog_describe.go implements `orun catalog describe <component>`: resolve
// one ComponentManifest from the selected object-model catalog and render the
// cli-surface.md §4 section list (text) or the full manifest + execution rows
// (--json under {manifest, executions}).
//
// Component selectors (§4):
//   - bare name (api-edge)               → resolved within the current catalog
//   - fully-qualified key (ns/repo/name) → exact componentKey match
//   - ambiguous bare name across repos   → exit 4 with the candidate list
//
// The manifest is reconstructed from the object-model component view: Identity,
// Spec, and Metadata are faithful (the Spec/Metadata maps round-trip back into
// the typed structs); Source carries the source/catalog keys; the Runtime-
// inference and Resolution-provenance sections are not recorded on the object-
// model component (a documented v1 gap — specs/orun-legacy-retirement Bucket 1)
// and render empty.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalogDescribeData is the --json payload: the full manifest plus the
// catalog-local execution rows for the component (§4).
type catalogDescribeData struct {
	Manifest   catalogmodel.ComponentManifest       `json:"manifest"`
	Executions []catalogmodel.ComponentExecutionRow `json:"executions"`
}

func registerCatalogDescribeCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "describe <component>",
		Short: "Show the full resolved manifest for one component",
		Long: `Show the full resolved manifest for one component in the selected catalog.

The component may be given as a bare name (resolved within the catalog) or as
a fully-qualified component key (namespace/repo/name) for an exact match. A
bare name that matches components in more than one repo exits 4 with the list
of candidate keys.

Examples:
  orun catalog describe api-edge
  orun catalog describe api-edge --source main
  orun catalog describe sourceplane/orun/api-edge
  orun catalog describe api-edge --json

Exit codes:
  0  Component found and rendered.
  1  Invalid selector or missing component argument.
  3  StateStore failure.
  4  Ambiguous bare name across repos.
  6  Catalog or component not found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogDescribe(cmd.Context(), args[0])
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Stable machine-readable output")

	parent.AddCommand(cmd)
}

func runCatalogDescribe(ctx context.Context, arg string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return exitErr(1, "describe requires a component name")
	}

	view, reader, err := loadObjCatalog(ctx)
	if err != nil {
		return err
	}

	c, err := selectObjComponent(view, arg)
	if err != nil {
		return err
	}
	manifest := buildObjManifest(view, c)

	// Execution rows from the object-model join (same source as `catalog
	// history`): executionKey/revision/status off the sealed execution.
	execViews, eerr := reader.ComponentExecutions(ctx, arg)
	if eerr != nil {
		return exitErr(3, "read executions: %w", eerr)
	}
	execs := make([]catalogmodel.ComponentExecutionRow, 0, len(execViews))
	for _, e := range execViews {
		execs = append(execs, catalogmodel.ComponentExecutionRow{
			ComponentKey: c.ComponentKey,
			RevisionKey:  e.RevisionID,
			ExecutionKey: e.ExecutionKey,
			Status:       e.Status,
			CreatedAt:    e.StartedAt.UTC().Format(time.RFC3339),
		})
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogDescribeResult, catalogDescribeData{
			Manifest:   manifest,
			Executions: execs,
		}, nil)
	}
	return renderCatalogDescribeText(manifest, execs)
}

// selectObjComponent resolves the §4 component selector against the object-model
// catalog view. A fully-qualified key (containing '/') is matched exactly; a
// bare name matches on Name and exits 4 when more than one repo supplies it.
func selectObjComponent(view objcatalog.CatalogView, arg string) (objcatalog.CatalogComponentView, error) {
	if strings.Contains(arg, "/") {
		for _, c := range view.Components {
			if c.ComponentKey == arg {
				return c, nil
			}
		}
		return objcatalog.CatalogComponentView{}, exitErr(6, "component %q not found in catalog", arg)
	}

	var matches []objcatalog.CatalogComponentView
	for _, c := range view.Components {
		if c.Name == arg {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return objcatalog.CatalogComponentView{}, exitErr(6, "component %q not found in catalog", arg)
	case 1:
		return matches[0], nil
	default:
		keys := make([]string, 0, len(matches))
		for _, c := range matches {
			keys = append(keys, c.ComponentKey)
		}
		sort.Strings(keys)
		return objcatalog.CatalogComponentView{}, exitErr(4,
			"component %q is ambiguous across repos; qualify with a full key: %s",
			arg, strings.Join(keys, ", "))
	}
}

// buildObjManifest reconstructs a typed catalogmodel.ComponentManifest from the
// object-model component view. Spec/Metadata are recovered by round-tripping the
// generic maps back into the typed structs (they were produced by JSON-encoding
// those same structs); Source carries the source/catalog keys. Runtime and
// Resolution are not recorded on the object-model component and stay zero.
func buildObjManifest(view objcatalog.CatalogView, c objcatalog.CatalogComponentView) catalogmodel.ComponentManifest {
	m := catalogmodel.ComponentManifest{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponentManifest,
		Identity: catalogmodel.ComponentIdentity{
			ComponentKey: c.ComponentKey,
			Name:         c.Name,
			Namespace:    c.Namespace,
			Repo:         c.Repo,
			Path:         c.Path,
		},
		Source: catalogmodel.ComponentSource{
			SourceSnapshotKey:  view.SourceID,
			CatalogSnapshotKey: objCatalogSnapshotKey(view),
		},
	}
	roundTripInto(c.Spec, &m.Spec)
	roundTripInto(c.Metadata, &m.Metadata)
	// Owner/maintainers/contacts moved out of metadata into the ownership block
	// in the SC1 envelope reshape; recover them so describe/diff see the full
	// manifest (losslessness, invariant 3).
	if c.Owner != "" {
		m.Metadata.Owner = c.Owner
	}
	if add := stringSliceField(c.Ownership, "additionalOwners"); len(add) > 0 {
		m.Metadata.Maintainers = add
	}
	if contacts := contactsField(c.Ownership); len(contacts) > 0 {
		m.Metadata.Contacts = contacts
	}
	return m
}

// stringSliceField reads a []string from a generic envelope block (e.g.
// ownership.additionalOwners), tolerating absent/typed-wrong values.
func stringSliceField(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// contactsField reconstructs the ownership.contacts list (type→value) into the
// flat map the catalogmodel manifest carries.
func contactsField(m map[string]any) map[string]string {
	raw, ok := m["contacts"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, v := range raw {
		c, ok := v.(map[string]any)
		if !ok {
			continue
		}
		t, _ := c["type"].(string)
		val, _ := c["value"].(string)
		if t != "" {
			out[t] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// roundTripInto re-decodes a generic map into a typed destination via JSON. Used
// to recover the typed Spec/Metadata from the object-model manifest's maps; a
// marshal/unmarshal error leaves dst at its zero value (best-effort render).
func roundTripInto(src, dst any) {
	b, err := json.Marshal(src)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, dst)
}

func renderCatalogDescribeText(m catalogmodel.ComponentManifest, execs []catalogmodel.ComponentExecutionRow) error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	out := os.Stdout

	section := func(title string) { fmt.Fprintf(out, "\n%s\n", ui.Bold(color, title)) }
	kv := func(k, v string) {
		if v != "" {
			fmt.Fprintf(out, "  %-14s %s\n", k+":", v)
		}
	}

	fmt.Fprintf(out, "%s\n", ui.Bold(color, m.Identity.Name))

	section("Component")
	kv("Key", m.Identity.ComponentKey)
	kv("Name", m.Identity.Name)
	kv("Type", m.Spec.Type)
	kv("Lifecycle", m.Spec.Lifecycle)
	kv("Title", m.Metadata.Title)
	kv("Description", m.Metadata.Description)

	section("Ownership")
	kv("Owner", m.Metadata.Owner)
	kv("System", m.Spec.System)
	kv("Domain", m.Spec.Domain)
	if len(m.Metadata.Maintainers) > 0 {
		kv("Maintainers", strings.Join(m.Metadata.Maintainers, ", "))
	}

	section("Source")
	kv("Source", m.Source.SourceSnapshotKey)
	kv("Catalog", m.Source.CatalogSnapshotKey)
	kv("Ref", m.Source.Ref)
	kv("Branch", m.Source.Branch)
	kv("Tree", m.Source.TreeHash)
	kv("WorkingTree", m.Source.WorkingTree)
	kv("ManifestHash", m.Source.ManifestHash)

	section("Environments")
	if len(m.Spec.Environments) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, name := range sortedEnvKeys(m.Spec.Environments) {
			env := m.Spec.Environments[name]
			active := "inactive"
			if env.Active {
				active = "active"
			}
			fmt.Fprintf(out, "  %-14s profile=%s (%s)\n", name, env.Profile, active)
		}
	}

	section("Dependencies")
	if len(m.Spec.Dependencies.Components) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, d := range m.Spec.Dependencies.Components {
			opt := ""
			if d.Optional {
				opt = " (optional)"
			}
			fmt.Fprintf(out, "  %s → %s [%s]%s\n", m.Identity.Name, d.Name, d.Relationship, opt)
		}
	}

	section("APIs")
	if len(m.Spec.Dependencies.APIs.Provides) > 0 {
		kv("Provides", strings.Join(m.Spec.Dependencies.APIs.Provides, ", "))
	}
	if len(m.Spec.Dependencies.APIs.Consumes) > 0 {
		kv("Consumes", strings.Join(m.Spec.Dependencies.APIs.Consumes, ", "))
	}
	if len(m.Spec.Dependencies.APIs.Provides) == 0 && len(m.Spec.Dependencies.APIs.Consumes) == 0 {
		fmt.Fprintln(out, "  (none)")
	}

	section("Resources")
	if len(m.Spec.Dependencies.Resources.Uses) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		kv("Uses", strings.Join(m.Spec.Dependencies.Resources.Uses, ", "))
	}

	section("Runtime inference")
	kv("Languages", strings.Join(m.Runtime.Inferred.Languages, ", "))
	kv("PackageMgrs", strings.Join(m.Runtime.Inferred.PackageManagers, ", "))
	kv("Frameworks", strings.Join(m.Runtime.Inferred.Frameworks, ", "))
	kv("Infra", strings.Join(m.Runtime.Inferred.Infra, ", "))

	section("Latest executions")
	if len(execs) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		rows := append([]catalogmodel.ComponentExecutionRow(nil), execs...)
		sort.SliceStable(rows, func(a, b int) bool { return rows[a].CreatedAt > rows[b].CreatedAt })
		for _, r := range rows {
			fmt.Fprintf(out, "  %-22s %-10s %s\n", r.ExecutionKey, r.Status, r.CreatedAt)
		}
	}

	section("Resolution provenance")
	if len(m.Resolution.InheritedFrom) == 0 && len(m.Resolution.InferredFrom) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, k := range sortedStringKeys(m.Resolution.InheritedFrom) {
			fmt.Fprintf(out, "  %-22s inherited from %s\n", k, m.Resolution.InheritedFrom[k])
		}
		for _, k := range sortedSliceMapKeys(m.Resolution.InferredFrom) {
			fmt.Fprintf(out, "  %-22s inferred from %s\n", k, strings.Join(m.Resolution.InferredFrom[k], ", "))
		}
	}
	return nil
}

func sortedEnvKeys(m map[string]catalogmodel.ComponentEnvironment) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSliceMapKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
