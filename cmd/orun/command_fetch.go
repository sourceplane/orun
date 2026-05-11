package main

import (
	"fmt"
	"os"
	"strings"

	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/spf13/cobra"
)

var (
	fetchOutput    string
	fetchOverwrite bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <oci-ref>",
	Short: "Download a composition package from an OCI registry to a local directory",
	Long: `Download and extract a composition package from an OCI registry.

The package is written to a local directory named after the image repository by
default, or to the path specified by --output.

Examples:
  orun fetch ghcr.io/sourceplane/stack-tectonic:0.12.0
  orun fetch ghcr.io/acme/aws-vpc:v1.4.0 --output ./my-vpc
  orun fetch ghcr.io/acme/aws-vpc:v1.4.0 --overwrite
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFetch(args[0])
	},
}

func registerFetchCommand(root *cobra.Command) {
	root.AddCommand(fetchCmd)
	fetchCmd.Flags().StringVarP(&fetchOutput, "output", "o", "", "Destination directory (defaults to ./<package-name>)")
	fetchCmd.Flags().BoolVar(&fetchOverwrite, "overwrite", false, "Overwrite destination directory if it already exists")
}

func runFetch(ociRef string) error {
	dest := fetchOutput
	if dest == "" {
		dest = inferFetchDestDir(ociRef)
	}

	if _, err := os.Stat(dest); err == nil {
		if !fetchOverwrite {
			return fmt.Errorf("destination %q already exists; use --overwrite to replace it", dest)
		}
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("failed to remove existing %s: %w", dest, err)
		}
	}

	fmt.Printf("□ Fetching %s...\n", ociRef)
	outDir, err := compositionpkg.FetchToDir(ociRef, dest)
	if err != nil {
		return err
	}
	fmt.Printf("✓ fetched\n")
	fmt.Printf("→ %s\n", outDir)
	return nil
}

// inferFetchDestDir derives a local directory name from an OCI ref.
// "ghcr.io/sourceplane/stack-tectonic:0.12.0" → "stack-tectonic"
func inferFetchDestDir(ociRef string) string {
	ref := strings.TrimPrefix(ociRef, "oci://")
	// Strip @digest
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	// Strip :tag (only when colon does not appear inside a path segment)
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		if !strings.ContainsRune(ref[idx:], '/') {
			ref = ref[:idx]
		}
	}
	// Take the last path segment
	if idx := strings.LastIndex(ref, "/"); idx != -1 {
		ref = ref[idx+1:]
	}
	if ref == "" {
		return "composition"
	}
	return ref
}
