package cli

import "testing"

func TestParseOptionsDefaultsToProviderManifest(t *testing.T) {
	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}

	if opts.ProviderPath != "provider.yaml" {
		t.Fatalf("expected default provider path provider.yaml, got %q", opts.ProviderPath)
	}

	if opts.DistDir != "dist" {
		t.Fatalf("expected default dist dir dist, got %q", opts.DistDir)
	}

	if opts.OutputDir != "oci" {
		t.Fatalf("expected default output dir oci, got %q", opts.OutputDir)
	}
}

func TestParseOptionsAcceptsOverrides(t *testing.T) {
	opts, err := parseOptions([]string{
		"--provider", "custom-provider.yaml",
		"--build-with", "goreleaser",
		"--dist", "custom-dist",
		"--output", "custom-oci",
		"--ref", "ghcr.io/sourceplane/lite-ci:test",
	})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}

	if opts.ProviderPath != "custom-provider.yaml" {
		t.Fatalf("expected custom provider path, got %q", opts.ProviderPath)
	}

	if opts.BuildWith != "goreleaser" {
		t.Fatalf("expected build-with goreleaser, got %q", opts.BuildWith)
	}

	if opts.DistDir != "custom-dist" {
		t.Fatalf("expected custom dist dir, got %q", opts.DistDir)
	}

	if opts.OutputDir != "custom-oci" {
		t.Fatalf("expected custom output dir, got %q", opts.OutputDir)
	}

	if opts.PublishRef != "ghcr.io/sourceplane/lite-ci:test" {
		t.Fatalf("expected publish ref override, got %q", opts.PublishRef)
	}
}
