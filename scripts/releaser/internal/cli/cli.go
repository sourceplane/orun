package cli

import (
	"flag"
	"fmt"

	"github.com/sourceplane/releaser/internal/release"
)

func parseOptions(args []string) (release.Options, error) {
	opts := release.Options{}

	fs := flag.NewFlagSet("releaser", flag.ContinueOnError)
	fs.StringVar(&opts.ProviderPath, "provider", "provider.yaml", "Path to provider manifest")
	fs.StringVar(&opts.BuildWith, "build-with", "", "Build tool to run before packaging (goreleaser|gorelaser)")
	fs.StringVar(&opts.DistDir, "dist", "dist", "GoReleaser dist directory")
	fs.StringVar(&opts.OutputDir, "output", "oci", "OCI layout output directory")
	fs.StringVar(&opts.PublishRef, "ref", "", "OCI reference to push to")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: releaser [flags]\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return release.Options{}, err
	}

	return opts, nil
}

func Execute(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	return release.Run(opts)
}
