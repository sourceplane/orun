package loader

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

const previewIntent = `apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: preview-test
discovery:
  roots: [apps]
preview:
  component: web
`

const previewComponent = `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web
spec:
  type: app
  preview:
    run: ["pnpm", "--filter", "web", "dev", "--port", "4173"]
    port: 4173
    needs: [api-edge]
    path: /app
    env:
      MODE: preview
`

func wantWebPreview() *model.ComponentPreview {
	return &model.ComponentPreview{
		Run:   []string{"pnpm", "--filter", "web", "dev", "--port", "4173"},
		Port:  4173,
		Needs: []string{"api-edge"},
		Path:  "/app",
		Env:   map[string]string{"MODE": "preview"},
	}
}

func findComponent(t *testing.T, intent *model.Intent, name string) model.Component {
	t.Helper()
	for _, c := range intent.Components {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("component %s not loaded", name)
	return model.Component{}
}

// spec.preview and the intent's preview survive the load and the
// component-tree cache: the cache once dropped every field it did not list.
func TestLoadResolvedIntent_PreviewThroughCache(t *testing.T) {
	root := t.TempDir()
	intentPath := filepath.Join(root, "intent.yaml")
	writeTestFile(t, intentPath, previewIntent)
	writeTestFile(t, filepath.Join(root, "apps", "web", "component.yaml"), previewComponent)

	intent, tree, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Preview == nil || intent.Preview.Component != "web" {
		t.Fatalf("intent preview = %+v", intent.Preview)
	}
	if got := findComponent(t, intent, "web").Preview; !reflect.DeepEqual(got, wantWebPreview()) {
		t.Fatalf("first load preview = %+v", got)
	}
	if err := WriteComponentTreeCache(intentPath, tree); err != nil {
		t.Fatal(err)
	}
	cache, err := os.ReadFile(filepath.Join(root, ".orun", "component-tree.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cache), "preview:") || !strings.Contains(string(cache), "format: 1") {
		t.Fatalf("cache lacks the preview or its format:\n%s", cache)
	}

	again, _, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := findComponent(t, again, "web").Preview; !reflect.DeepEqual(got, wantWebPreview()) {
		t.Fatalf("cached load preview = %+v", got)
	}
}

// A cache an older orun wrote (no format) is not trusted: it would lack
// spec.preview, so the manifests are read again.
func TestLoadResolvedIntent_OldCacheFormatIsAMiss(t *testing.T) {
	root := t.TempDir()
	intentPath := filepath.Join(root, "intent.yaml")
	writeTestFile(t, intentPath, previewIntent)
	manifest := filepath.Join(root, "apps", "web", "component.yaml")
	writeTestFile(t, manifest, previewComponent)

	_, tree, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	// What an older orun wrote: same file stamps, no format, no preview.
	tree.Metadata.Format = 0
	for i := range tree.Components {
		tree.Components[i].Preview = nil
	}
	if err := WriteComponentTreeCache(intentPath, tree); err != nil {
		t.Fatal(err)
	}

	intent, _, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := findComponent(t, intent, "web").Preview; !reflect.DeepEqual(got, wantWebPreview()) {
		t.Fatalf("an old-format cache was trusted: preview = %+v", got)
	}
}
