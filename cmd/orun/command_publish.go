package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/spf13/cobra"
)

var (
	publishRoot     string
	publishVersion  string
	publishDryRun   bool
	publishKeep     bool
	packRoot        string
	packOutput      string
	loginUsername   string
	loginPassword   string
	loginPwdStdin   bool
)

var publishCmd = &cobra.Command{
	Use:   "publish [oci-ref]",
	Short: "Package and publish a composition package to an OCI registry",
	Long: `Publish a composition package to an OCI registry.

With zero arguments, orun infers the registry and repository from the local
git remote (ghcr.io/<owner>/<repo>) and the version from the latest matching
git tag (or the manifest's spec.version, or a 0.1.0-dev+sha placeholder).

Examples:
  orun publish
  orun publish ghcr.io/acme/aws-vpc
  orun publish ghcr.io/acme/aws-vpc:v1.4.0
  orun publish --dry-run
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		return runPublish(target)
	},
}

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Build a composition package archive (no upload)",
	Long:  "Build a .tgz archive of a composition package directory containing orun.yaml.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPack()
	},
}

var loginCmd = &cobra.Command{
	Use:   "login [registry]",
	Short: "Authenticate to an OCI registry",
	Long: `Store credentials for an OCI registry.

Examples:
  orun login ghcr.io
  orun login ghcr.io --username acme --password-stdin < token.txt
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		registry := normalizeRegistry(args[0])
		fmt.Printf("□ Logging in to %s...\n", registry)
		if err := compositionpkg.LoginToRegistry(registry, loginUsername, loginPassword, loginPwdStdin); err != nil {
			return err
		}
		fmt.Printf("✓ Logged in to %s\n", registry)
		return nil
	},
}

func registerPublishCommand(root *cobra.Command) {
	root.AddCommand(publishCmd)
	root.AddCommand(packCmd)
	root.AddCommand(loginCmd)

	publishCmd.Flags().StringVar(&publishRoot, "root", "", "Composition package root (defaults to ./ or ./examples/compositions)")
	publishCmd.Flags().StringVar(&publishVersion, "version", "", "Override version tag (defaults to git tag, then manifest spec.version)")
	publishCmd.Flags().BoolVar(&publishDryRun, "dry-run", false, "Resolve target and build archive but skip upload")
	publishCmd.Flags().BoolVar(&publishKeep, "keep-archive", false, "Keep the built archive after publish")

	packCmd.Flags().StringVar(&packRoot, "root", "", "Composition package root (defaults to ./)")
	packCmd.Flags().StringVarP(&packOutput, "output", "o", "", "Output archive path (default: ./<name>-<version>.tgz)")

	loginCmd.Flags().StringVarP(&loginUsername, "username", "u", "", "Registry username")
	loginCmd.Flags().StringVarP(&loginPassword, "password", "p", "", "Registry password (prefer --password-stdin)")
	loginCmd.Flags().BoolVar(&loginPwdStdin, "password-stdin", false, "Read password from stdin")
}

func runPack() error {
	root := defaultPackageRoot(packRoot)
	plan, err := compositionpkg.ResolvePackPlan(root, "")
	if err != nil {
		return err
	}

	output := strings.TrimSpace(packOutput)
	if output == "" {
		output = filepath.Join(".", fmt.Sprintf("%s-%s.tgz", plan.PackageName, plan.Version))
	}

	fmt.Println("□ Building composition package...")
	if err := compositionpkg.BuildPackageArchive(plan.PackageRoot, output); err != nil {
		return err
	}
	printPackResult(plan, output)
	return nil
}

func runPublish(target string) error {
	root := defaultPackageRoot(publishRoot)
	plan, err := compositionpkg.ResolvePublishPlan(root, target, publishVersion)
	if err != nil {
		return err
	}

	printPublishHeader(plan)

	if publishDryRun {
		fmt.Printf("\n✓ dry run: no files uploaded\n")
		fmt.Printf("→ would push to %s\n", plan.OCIRef)
		return nil
	}

	fmt.Println("\n□ Packaging and uploading...")
	if err := compositionpkg.StreamPublishPackage(plan.PackageRoot, plan.OCIRef); err != nil {
		return err
	}

	if publishKeep {
		archiveName := fmt.Sprintf("%s-%s.tgz", plan.PackageName, plan.Version)
		if err := compositionpkg.BuildPackageArchive(plan.PackageRoot, archiveName); err != nil {
			return fmt.Errorf("failed to write archive: %w", err)
		}
		fmt.Printf("→ archive kept at %s\n", archiveName)
	}

	fmt.Println("\n✓ published")
	fmt.Printf("→ %s\n", plan.OCIRef)
	return nil
}

func printPublishHeader(plan *compositionpkg.PublishPlan) {
	fmt.Println()
	fmt.Printf("  package    %s\n", plan.PackageName)
	fmt.Printf("  version    %s\n", plan.Version)
	fmt.Printf("  files      %d\n", plan.FileCount)
	fmt.Printf("  registry   %s/%s\n", plan.Registry, plan.Repository)
	if plan.InferredFromGit {
		fmt.Println("  source     inferred from git remote")
	}
}

func printPackResult(plan *compositionpkg.PublishPlan, output string) {
	fmt.Println()
	fmt.Printf("  package    %s\n", plan.PackageName)
	fmt.Printf("  version    %s\n", plan.Version)
	fmt.Printf("  files      %d\n", plan.FileCount)
	fmt.Printf("  archive    %s\n", output)
	fmt.Println("\n✓ packed")
}

func defaultPackageRoot(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if _, err := os.Stat("orun.yaml"); err == nil {
		return "."
	}
	if _, err := os.Stat(filepath.Join("examples", "compositions", "orun.yaml")); err == nil {
		return filepath.Join("examples", "compositions")
	}
	return "."
}

func normalizeRegistry(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "oci://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.TrimRight(s, "/")
}
