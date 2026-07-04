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
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalogDescribeKindFlag addresses a non-Component entity kind (WO3.1b).
var catalogDescribeKindFlag string

// catalogDescribeData is the --json payload: the full manifest plus the
// catalog-local execution rows for the component (§4).
type catalogDescribeData struct {
	Manifest    catalogmodel.ComponentManifest       `json:"manifest"`
	Executions  []catalogmodel.ComponentExecutionRow `json:"executions"`
	Deployments []objread.Deployment                 `json:"deployments,omitempty"`
}

// catalogDescribeEntityData is the --json payload for a non-Component entity
// (WO3.1b): the entity envelope under `entity`, matching cli-surface.md §2.
type catalogDescribeEntityData struct {
	Entity catalogEntityJSON `json:"entity"`
}

// catalogEntityJSON is the stable machine-readable projection of an EntityView.
type catalogEntityJSON struct {
	Kind        string           `json:"kind"`
	EntityKey   string           `json:"entityKey"`
	Name        string           `json:"name"`
	Namespace   string           `json:"namespace,omitempty"`
	Repo        string           `json:"repo,omitempty"`
	DisplayName string           `json:"displayName,omitempty"`
	Description string           `json:"description,omitempty"`
	Owner       string           `json:"owner,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Lifecycle   string           `json:"lifecycle,omitempty"`
	Version     string           `json:"version,omitempty"`
	MemberCount int              `json:"memberCount"`
	Members     []string         `json:"members,omitempty"`
	Links       []map[string]any `json:"links,omitempty"`
	Docs        map[string]any   `json:"docs,omitempty"`
}

func entityJSON(e objcatalog.EntityView) catalogEntityJSON {
	return catalogEntityJSON{
		Kind: e.Kind, EntityKey: e.EntityKey, Name: e.Name, Namespace: e.Namespace,
		Repo: e.Repo, DisplayName: e.DisplayName, Description: e.Description,
		Owner: e.Owner, Tags: e.Tags, Lifecycle: e.Lifecycle, Version: e.Version,
		MemberCount: e.MemberCount, Members: e.Members, Links: e.Links, Docs: e.Docs,
	}
}

func registerCatalogDescribeCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "describe <entity>",
		Short: "Show the full resolved manifest for one component or entity",
		Long: `Show the full resolved envelope for one entity in the selected catalog.

Components are the default. Other entity kinds (Repo, System, Domain, API,
Resource, Group, Composition, Environment) are addressed with a kind-prefixed
selector or --kind:

  - bare name / key           → a Component (unchanged behavior)
  - <kind>:<key>              → that entity, e.g. repo:sourceplane/ogpic/ogpic
  - --kind <Kind> <name|key>  → that entity by name or full key
  - bare kind keyword         → the single entity of that kind (e.g. "repo",
                                which is one-per-snapshot)

A bare name (component or entity) that matches more than one candidate exits 4
with the list of candidate keys.

Examples:
  orun catalog describe api-edge
  orun catalog describe sourceplane/orun/api-edge
  orun catalog describe repo:sourceplane/ogpic/ogpic
  orun catalog describe repo               # the one Repo entity
  orun catalog describe --kind System payments
  orun catalog describe repo:sourceplane/ogpic/ogpic --json

Exit codes:
  0  Component or entity found and rendered.
  1  Invalid selector or missing argument.
  3  StateStore failure.
  4  Ambiguous name across repos/kinds.
  6  Catalog, component, or entity not found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogDescribe(cmd.Context(), args[0])
		},
	}

	addCatalogSelectorFlags(cmd)
	cmd.Flags().StringVar(&catalogDescribeKindFlag, "kind", "", "Address an entity of this kind (Repo|System|Domain|API|Resource|Group|Composition|Environment) instead of a Component")
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

	// Kind-aware routing (WO3.1b): a <kind>:<key> selector, --kind, or a bare
	// kind keyword addresses a non-Component entity. A bare kind keyword defers
	// to a same-named Component when one exists (back-compat).
	if kind, key, isEntity := parseEntitySelector(arg, catalogDescribeKindFlag); isEntity {
		if key == "" && componentNamed(view, arg) {
			// fall through to the component path below
		} else {
			e, serr := selectObjEntity(view, kind, key)
			if serr != nil {
				return serr
			}
			if catalogJSONFlag {
				return writeCatalogEnvelope(kindCatalogDescribeResult, catalogDescribeEntityData{Entity: entityJSON(e)}, nil)
			}
			return renderCatalogEntityText(e)
		}
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

	// Live plane (L2): the latest deployment per environment, derived on read
	// from objrun execution truth (never persisted — CR-1).
	deployments, derr := reader.ComponentDeployments(ctx, arg)
	if derr != nil {
		return exitErr(3, "read deployments: %w", derr)
	}

	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogDescribeResult, catalogDescribeData{
			Manifest:    manifest,
			Executions:  execs,
			Deployments: deployments,
		}, nil)
	}
	return renderCatalogDescribeText(manifest, execs, deployments)
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
	// Catalog-hub blocks (SC6) carried verbatim from the envelope.
	m.Integrations = c.Integrations
	m.Extensions = c.Extensions
	roundTripInto(c.Docs, &m.Docs)
	roundTripInto(c.Links, &m.Links)
	// Inferred runtime now lives in the envelope spec.runtime — recover it so the
	// "Runtime inference" describe section renders (previously a documented gap).
	if rt, ok := c.Spec["runtime"].(map[string]any); ok {
		roundTripInto(rt, &m.Runtime.Inferred)
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

// compositionLabel renders the composition binding as "source@version (lifecycle)"
// when those are known, else just the source.
func compositionLabel(c catalogmodel.CompositionRef) string {
	if c.Source == "" {
		return ""
	}
	out := c.Source
	if c.Version != "" {
		out += "@" + c.Version
	}
	if c.Lifecycle != "" {
		out += " (" + c.Lifecycle + ")"
	}
	return out
}

func renderCatalogDescribeText(m catalogmodel.ComponentManifest, execs []catalogmodel.ComponentExecutionRow, deployments []objread.Deployment) error {
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
	kv("Composition", compositionLabel(m.Spec.Composition)) // the golden path backing this component (SC7)
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

	if len(m.Integrations) > 0 {
		section("Integrations")
		for _, k := range sortedAnyMapKeys(m.Integrations) {
			fmt.Fprintf(out, "  %s\n", k)
		}
	}
	if len(m.Extensions) > 0 {
		section("Extensions")
		for _, k := range sortedAnyMapKeys(m.Extensions) {
			fmt.Fprintf(out, "  %s\n", k)
		}
	}

	section("Runtime inference")
	kv("Languages", strings.Join(m.Runtime.Inferred.Languages, ", "))
	kv("PackageMgrs", strings.Join(m.Runtime.Inferred.PackageManagers, ", "))
	kv("Frameworks", strings.Join(m.Runtime.Inferred.Frameworks, ", "))
	kv("Infra", strings.Join(m.Runtime.Inferred.Infra, ", "))

	// L2 live plane: the latest deployment per environment, derived on read from
	// objrun (orun-service-catalog design.md §6). Never persisted into the L1
	// manifest (CR-1).
	section("Live deployments")
	if len(deployments) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, d := range deployments {
			when := ""
			if !d.DeployedAt.IsZero() {
				when = d.DeployedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(out, "  %-14s %-10s health=%-9s %s\n", d.Environment, d.Status, d.Health, when)
		}
	}

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

// parseEntitySelector decides whether arg addresses a non-Component entity and,
// if so, returns its canonical kind and key. Three entity forms are recognized
// (§WO3.1b): a `<kind>:<key>` prefix, the --kind flag, or a bare kind keyword
// (key == "" → the single entity of that kind). Anything else routes to the
// Component path (isEntity == false), preserving `describe <component>`.
func parseEntitySelector(arg, kindFlag string) (kind, key string, isEntity bool) {
	if i := strings.IndexByte(arg, ':'); i > 0 {
		if k := canonicalKindCase(arg[:i]); catalogmodel.IsEntityKind(k) {
			if norm := catalogmodel.NormalizeEntityKind(k); norm != catalogmodel.EntityKindComponent {
				return norm, arg[i+1:], true
			}
		}
	}
	if kindFlag != "" {
		norm := catalogmodel.NormalizeEntityKind(canonicalKindCase(kindFlag))
		if norm == catalogmodel.EntityKindComponent {
			return "", arg, false
		}
		return norm, arg, true
	}
	if !strings.Contains(arg, "/") {
		if k := canonicalKindCase(arg); catalogmodel.IsEntityKind(k) {
			if norm := catalogmodel.NormalizeEntityKind(k); norm != catalogmodel.EntityKindComponent {
				return norm, "", true
			}
		}
	}
	return "", arg, false
}

// canonicalKindCase maps a case-insensitive kind token onto its canonical
// spelling (repo→Repo, api→API), so `repo:` and `--kind repo` both resolve.
func canonicalKindCase(s string) string {
	for _, k := range catalogmodel.AllEntityKinds() {
		if strings.EqualFold(k, s) {
			return k
		}
	}
	if strings.EqualFold(s, catalogmodel.EntityKindOwner) {
		return catalogmodel.EntityKindOwner
	}
	return s
}

// componentNamed reports whether a component with this exact name exists — used
// so a bare kind keyword defers to a same-named Component (back-compat).
func componentNamed(view objcatalog.CatalogView, name string) bool {
	for _, c := range view.Components {
		if c.Name == name {
			return true
		}
	}
	return false
}

// selectObjEntity resolves a non-Component selector against view.Entities.
// An empty key selects the single entity of the kind (exit 4 if more than one);
// otherwise it matches an exact entityKey, then a bare name (exit 4 if the name
// is ambiguous, exit 6 if absent).
func selectObjEntity(view objcatalog.CatalogView, kind, key string) (objcatalog.EntityView, error) {
	var ofKind []objcatalog.EntityView
	for _, e := range view.Entities {
		if e.Kind == kind {
			ofKind = append(ofKind, e)
		}
	}
	if len(ofKind) == 0 {
		return objcatalog.EntityView{}, exitErr(6, "no %s entity in catalog", kind)
	}
	if key == "" {
		if len(ofKind) == 1 {
			return ofKind[0], nil
		}
		return objcatalog.EntityView{}, exitErr(4,
			"%s is ambiguous (%d in catalog); qualify with a key: %s",
			kind, len(ofKind), strings.Join(entityKeysOf(ofKind), ", "))
	}
	for _, e := range ofKind {
		if e.EntityKey == key {
			return e, nil
		}
	}
	var matches []objcatalog.EntityView
	for _, e := range ofKind {
		if e.Name == key {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return objcatalog.EntityView{}, exitErr(6, "%s %q not found in catalog", kind, key)
	case 1:
		return matches[0], nil
	default:
		return objcatalog.EntityView{}, exitErr(4,
			"%s %q is ambiguous; qualify with a full key: %s",
			kind, key, strings.Join(entityKeysOf(matches), ", "))
	}
}

func entityKeysOf(es []objcatalog.EntityView) []string {
	keys := make([]string, 0, len(es))
	for _, e := range es {
		keys = append(keys, e.EntityKey)
	}
	sort.Strings(keys)
	return keys
}

// renderCatalogEntityText renders one non-Component entity's envelope: identity,
// ownership, tags, docs (the overview doc_ref shows path + digest), links, and
// membership.
func renderCatalogEntityText(e objcatalog.EntityView) error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	out := os.Stdout

	section := func(title string) { fmt.Fprintf(out, "\n%s\n", ui.Bold(color, title)) }
	kv := func(k, v string) {
		if v != "" {
			fmt.Fprintf(out, "  %-14s %s\n", k+":", v)
		}
	}

	name := e.DisplayName
	if name == "" {
		name = e.Name
	}
	fmt.Fprintf(out, "%s\n", ui.Bold(color, name))

	section(e.Kind)
	kv("Key", e.EntityKey)
	kv("Name", e.Name)
	kv("DisplayName", e.DisplayName)
	kv("Description", e.Description)
	kv("Namespace", e.Namespace)
	kv("Repo", e.Repo)
	kv("Version", e.Version)
	kv("Lifecycle", e.Lifecycle)

	section("Ownership")
	if e.Owner == "" {
		fmt.Fprintln(out, "  (none)")
	} else {
		kv("Owner", e.Owner)
	}

	if len(e.Tags) > 0 {
		section("Tags")
		fmt.Fprintf(out, "  %s\n", strings.Join(e.Tags, ", "))
	}

	section("Docs")
	if len(e.Docs) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, k := range sortedAnyMapKeys(e.Docs) {
			switch v := e.Docs[k].(type) {
			case map[string]any:
				path := anyString(v["path"])
				digest := anyString(v["digest"])
				if digest != "" {
					fmt.Fprintf(out, "  %-14s %s (%s)\n", k+":", path, digest)
				} else {
					fmt.Fprintf(out, "  %-14s %s\n", k+":", path)
				}
			default:
				fmt.Fprintf(out, "  %-14s %v\n", k+":", v)
			}
		}
	}

	if len(e.Links) > 0 {
		section("Links")
		for _, l := range e.Links {
			title := anyString(l["title"])
			url := anyString(l["url"])
			fmt.Fprintf(out, "  %-14s %s\n", title, url)
		}
	}

	section("Members")
	if e.MemberCount == 0 && len(e.Members) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, m := range e.Members {
			fmt.Fprintf(out, "  %s\n", m)
		}
	}
	return nil
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
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

func sortedAnyMapKeys(m map[string]any) []string {
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
