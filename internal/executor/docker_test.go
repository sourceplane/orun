package executor

import "testing"

func TestResolveDockerImage(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                    DefaultDockerImage,
		"ubuntu-22.04":        DefaultDockerImage,
		"ubuntu-24.04":        "ubuntu:24.04",
		"ubuntu-latest":       "ubuntu:latest",
		"ghcr.io/acme/ci:1.0": "ghcr.io/acme/ci:1.0",
	}

	for input, want := range tests {
		input := input
		want := want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := ResolveDockerImage(input); got != want {
				t.Fatalf("ResolveDockerImage(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
