package main

// catalog_migrate.go implements `orun catalog migrate`: a read-only lint pass
// that scores each authored component.yaml for orun.io/v1 readiness and reports
// the gaps (orun-service-catalog SC11, migration.md). It is advisory — it never
// rewrites files; the developer adopts the suggestions (a CODEOWNERS entry, a
// lifecycle stage, an orun.io apiVersion) at their own pace.
//
// Because the v1 authoring is additive (every new block is optional and the
// resolver derives what it can), migration is non-destructive: an un-migrated
// component still resolves. This command surfaces what would make it a
// first-class v1 entity.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// catalogMigrateFinding is one lint finding for a component.
type catalogMigrateFinding struct {
	Component string `json:"component"`
	File      string `json:"file"`
	Code      string `json:"code"`
	Severity  string `json:"severity"` // recommend | info
	Message   string `json:"message"`
}

// catalogMigrateData is the CatalogMigrateResult `data` payload.
type catalogMigrateData struct {
	Components int                     `json:"components"`
	Ready      int                     `json:"ready"` // components with no recommend-level findings
	Findings   []catalogMigrateFinding `json:"findings"`
}

func registerCatalogMigrateCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Lint authored component.yaml files for orun.io/v1 readiness",
		Long: `Lint the workspace's authored component.yaml files for orun.io/v1 readiness.

migrate is read-only and advisory: it never rewrites files. It reports, per
component, the gaps that keep it from being a first-class v1 catalog entity —
a missing owner (no authored owner and no CODEOWNERS match), a legacy
apiVersion, a missing lifecycle stage, or a missing system. v1 authoring is
additive, so an un-migrated component still resolves; this is the paved path
to adopt the richer model.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogMigrate(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&catalogJSONFlag, "json", false, "Emit the CatalogMigrateResult JSON envelope")
	parent.AddCommand(cmd)
}

func runCatalogMigrate(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := catalogWorkspaceRoot()
	if err != nil {
		return exitErr(3, "resolve workspace root: %w", err)
	}
	disc, err := catalogresolve.DiscoverAndLoad(ctx, catalogresolve.Options{WorkspaceRoot: root})
	if err != nil {
		return exitErr(3, "discover components: %w", err)
	}
	owners := objplan.OwnerResolverForWorkspace(root)

	var findings []catalogMigrateFinding
	ready := 0
	for _, am := range disc.Manifests {
		fs := lintComponentForV1(am, owners)
		if !hasRecommend(fs) {
			ready++
		}
		findings = append(findings, fs...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Component != findings[j].Component {
			return findings[i].Component < findings[j].Component
		}
		return findings[i].Code < findings[j].Code
	})

	data := catalogMigrateData{Components: len(disc.Manifests), Ready: ready, Findings: findings}
	if catalogJSONFlag {
		return writeCatalogEnvelope(kindCatalogMigrateResult, data, nil)
	}
	return renderCatalogMigrateText(data)
}

// lintComponentForV1 produces the v1-readiness findings for one authored
// component.yaml.
func lintComponentForV1(am catalogresolve.AuthoredManifest, owners objplan.OwnerResolver) []catalogMigrateFinding {
	c := am.Component
	name := c.Metadata.Name
	if name == "" {
		name = am.SourceFile
	}
	var out []catalogMigrateFinding
	add := func(code, severity, msg string) {
		out = append(out, catalogMigrateFinding{Component: name, File: am.SourceFile, Code: code, Severity: severity, Message: msg})
	}

	// apiVersion: recommend the graduated orun.io/v1.
	if !strings.HasPrefix(c.APIVersion, "orun.io/") {
		add("legacy-apiversion", "info", fmt.Sprintf("apiVersion %q — consider orun.io/v1", c.APIVersion))
	}
	// ownership: a missing owner with no CODEOWNERS fallback is the headline gap.
	ownedByCODEOWNERS := owners != nil && len(owners(am.SourceFile)) > 0
	if c.Spec.Owner == "" && !ownedByCODEOWNERS {
		add("unowned", "recommend", "no authored owner and no CODEOWNERS match — add a CODEOWNERS entry or spec.owner")
	}
	// lifecycle stage.
	if c.Spec.Lifecycle == "" {
		add("no-lifecycle", "recommend", "no lifecycle stage — add spec.lifecycle (experimental|production|deprecated|retired)")
	}
	// system membership (optional but recommended for the graph).
	if c.Spec.System == "" {
		add("no-system", "info", "no system — add spec.system to place this component in a System")
	}
	return out
}

func hasRecommend(fs []catalogMigrateFinding) bool {
	for _, f := range fs {
		if f.Severity == "recommend" {
			return true
		}
	}
	return false
}

func renderCatalogMigrateText(d catalogMigrateData) error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	out := os.Stdout
	fmt.Fprintf(out, "%s\n", ui.Bold(color, "Catalog v1 migration readiness"))
	fmt.Fprintf(out, "  %d components · %d ready · %d with recommendations\n", d.Components, d.Ready, d.Components-d.Ready)
	if len(d.Findings) == 0 {
		fmt.Fprintln(out, "\n✓ every component is v1-ready")
		return nil
	}
	current := ""
	for _, f := range d.Findings {
		if f.Component != current {
			fmt.Fprintf(out, "\n%s  %s\n", ui.Bold(color, f.Component), f.File)
			current = f.Component
		}
		marker := "·"
		if f.Severity == "recommend" {
			marker = "▲"
		}
		fmt.Fprintf(out, "  %s [%s] %s\n", marker, f.Code, f.Message)
	}
	return nil
}
