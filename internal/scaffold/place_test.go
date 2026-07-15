package scaffold

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func mkValues(fields map[string]any) Values {
	return Values{Fields: fields, secrets: map[string]string{}}
}

func TestPlaceInlineTemplate(t *testing.T) {
	m := Module{
		Name: "worker",
		Mode: ModeTemplate,
		Files: map[string]string{
			"apps/{{ .name }}/component.yaml": "metadata:\n  name: {{ .name }}\n",
			"apps/{{ .name }}/README.md":      "# {{ upper .name }}\n",
		},
	}
	out, err := placeModule(m, nil, mkValues(map[string]any{"name": "billing"}))
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	got := map[string]string{}
	for _, f := range out.files {
		got[f.Path] = string(f.Bytes)
	}
	if got["apps/billing/component.yaml"] != "metadata:\n  name: billing\n" {
		t.Errorf("component.yaml = %q", got["apps/billing/component.yaml"])
	}
	if got["apps/billing/README.md"] != "# BILLING\n" {
		t.Errorf("README = %q", got["apps/billing/README.md"])
	}
}

func TestPlaceCopyVerbatim(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "assets/logo.svg", "<svg>{{ not rendered }}</svg>")
	m := Module{Name: "assets", Mode: ModeCopy, Source: "base", From: "assets", To: "static"}
	out, err := placeModule(m, osTree{root: dir}, mkValues(map[string]any{}))
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if len(out.files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.files))
	}
	if out.files[0].Path != "static/logo.svg" {
		t.Errorf("path = %q", out.files[0].Path)
	}
	if !bytes.Equal(out.files[0].Bytes, []byte("<svg>{{ not rendered }}</svg>")) {
		t.Errorf("copy was rendered: %q", out.files[0].Bytes)
	}
}

func TestPlaceConsumeEmitsNothing(t *testing.T) {
	m := Module{Name: "contracts", Mode: ModeConsume, Source: "base", From: "packages/contracts"}
	out, err := placeModule(m, nil, mkValues(map[string]any{}))
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if len(out.files) != 0 {
		t.Fatalf("consume emitted %d files", len(out.files))
	}
	if out.consumed == nil || out.consumed.From != "packages/contracts" {
		t.Fatalf("consume dep not recorded: %+v", out.consumed)
	}
}

func TestPlaceDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "svc/main.txt", "svc {{ .name }} n={{ .n }}")
	writeFile(t, dir, "svc/config.yaml", "name: {{ .name }}")
	m := Module{Name: "svc", Mode: ModeTemplate, Source: "base", From: "svc", To: "out",
		Bind: []string{"main.txt", "config.yaml"}}
	vals := mkValues(map[string]any{"name": "alpha", "n": float64(2)})
	first, err := placeModule(m, osTree{root: dir}, vals)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	for i := 0; i < 25; i++ {
		again, err := placeModule(m, osTree{root: dir}, vals)
		if err != nil {
			t.Fatalf("place: %v", err)
		}
		if !placedEqual(first.files, again.files) {
			t.Fatal("placement is non-deterministic")
		}
	}
}

func TestPlaceRejectsEscapingTarget(t *testing.T) {
	for _, to := range []string{"../escape", "/abs/path", "a/../../b"} {
		m := Module{Name: "x", Mode: ModeTemplate, Files: map[string]string{to + "/f.txt": "hi"}}
		_, err := placeModule(m, nil, mkValues(map[string]any{}))
		if err == nil {
			t.Errorf("expected containment rejection for to=%q", to)
		}
	}
}

func TestPlaceBindLint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "svc/sneaky.txt", "value {{ .secretish }}") // references inputs, not in bind
	m := Module{Name: "svc", Mode: ModeTemplate, Source: "base", From: "svc", To: "out", Bind: []string{}}
	_, err := placeModule(m, osTree{root: dir}, mkValues(map[string]any{"secretish": "x"}))
	if err == nil {
		t.Fatal("expected bind lint error for non-bind file interpolating inputs")
	}
}

func TestPlaceSecretSweepTemplate(t *testing.T) {
	m := Module{Name: "svc", Mode: ModeTemplate, Files: map[string]string{
		"config.yaml": "token: {{ .apiToken }}",
	}}
	vals := Values{Fields: map[string]any{"apiToken": "s3cr3t"}, secrets: map[string]string{"apiToken": "s3cr3t"}}
	_, err := placeModule(m, nil, vals)
	if err == nil {
		t.Fatal("expected secret sweep to reject interpolated secret")
	}
}

func TestPlaceSecretSweepCopy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f/leaked.txt", "hardcoded s3cr3t here")
	m := Module{Name: "f", Mode: ModeCopy, Source: "base", From: "f", To: "f"}
	vals := Values{Fields: map[string]any{}, secrets: map[string]string{"apiToken": "s3cr3t"}}
	_, err := placeModule(m, osTree{root: dir}, vals)
	if err == nil {
		t.Fatal("expected secret sweep to reject copy bytes matching a secret")
	}
}

// helpers

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func placedEqual(a, b []PlacedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || !bytes.Equal(a[i].Bytes, b[i].Bytes) {
			return false
		}
	}
	return true
}

func TestPlaceSingleFileCopy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"lumen"}`)
	// From names a single file; To is the exact target path.
	m := Module{Name: "root-pkg", Mode: ModeCopy, Source: "base", From: "package.json", To: "package.json"}
	out, err := placeModule(m, osTree{root: dir}, mkValues(map[string]any{}))
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if len(out.files) != 1 || out.files[0].Path != "package.json" {
		t.Fatalf("single-file copy placed wrong: %+v", out.files)
	}
	if string(out.files[0].Bytes) != `{"name":"lumen"}` {
		t.Fatalf("bytes = %q", out.files[0].Bytes)
	}
}
