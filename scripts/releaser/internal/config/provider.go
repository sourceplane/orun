package config

import (
	"errors"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type PlatformManifest struct {
	OS     string `yaml:"os"`
	Arch   string `yaml:"arch"`
	Binary string `yaml:"binary"`
}

type ProviderManifest struct {
	Metadata struct {
		Name        string `yaml:"name"`
		Namespace   string `yaml:"namespace"`
		Version     string `yaml:"version"`
		Description string `yaml:"description"`
		Homepage    string `yaml:"homepage"`
		License     string `yaml:"license"`
	} `yaml:"metadata"`
	Distribution struct {
		ArtifactType string `yaml:"artifactType"`
	} `yaml:"distribution"`
	Goreleaser struct {
		Config string `yaml:"config"`
	} `yaml:"goreleaser"`
	Spec struct {
		Runtime    string             `yaml:"runtime"`
		Entrypoint string             `yaml:"entrypoint"`
		Platforms  []PlatformManifest `yaml:"platforms"`
		Layers     struct {
			Assets struct {
				Root string `yaml:"root"`
			} `yaml:"assets"`
		} `yaml:"layers"`
	} `yaml:"spec"`
	Entrypoint struct {
		Executable string `yaml:"executable"`
	} `yaml:"entrypoint"`
	Assets struct {
		Root string `yaml:"root"`
	} `yaml:"assets"`
	Platforms []PlatformManifest `yaml:"platforms"`
	Layers    struct {
		Core struct {
			MediaType       string `yaml:"mediaType"`
			AssetsMediaType string `yaml:"assetsMediaType"`
		} `yaml:"core"`
		Binaries map[string]struct {
			MediaType string `yaml:"mediaType"`
			Platform  string `yaml:"platform"`
		} `yaml:"binaries"`
		Examples struct {
			Includes  []string `yaml:"includes"`
			MediaType string   `yaml:"mediaType"`
		} `yaml:"examples"`
	} `yaml:"layers"`
}

func (m *ProviderManifest) normalize() {
	if m.Entrypoint.Executable == "" {
		m.Entrypoint.Executable = strings.TrimSpace(m.Spec.Entrypoint)
	}

	if m.Assets.Root == "" {
		m.Assets.Root = strings.TrimSpace(m.Spec.Layers.Assets.Root)
	}

	if len(m.Platforms) == 0 {
		m.Platforms = m.Spec.Platforms
	}
}

func LoadProviderManifest(path string) (ProviderManifest, error) {
	var manifest ProviderManifest

	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}

	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}

	manifest.normalize()

	if len(manifest.Platforms) == 0 {
		return manifest, errors.New("no platforms declared")
	}

	return manifest, nil
}
