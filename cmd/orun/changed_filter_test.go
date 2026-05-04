package main

import (
	"testing"
)

func TestIsPathChanged_DotSlashPrefix(t *testing.T) {
	files := map[string]struct{}{
		"services/api/main.go":    {},
		"services/web/src/app.js": {},
	}

	if !isPathChanged(files, "./services/api") {
		t.Error("expected ./services/api to match services/api/main.go")
	}
	if !isPathChanged(files, "./services/web") {
		t.Error("expected ./services/web to match services/web/src/app.js")
	}
	if isPathChanged(files, "./services/db") {
		t.Error("expected ./services/db to not match any file")
	}
}

func TestIsPathChanged_DotSlashInFiles(t *testing.T) {
	files := map[string]struct{}{
		"./services/api/main.go": {},
	}

	if !isPathChanged(files, "services/api") {
		t.Error("expected services/api to match ./services/api/main.go")
	}
	if !isPathChanged(files, "./services/api") {
		t.Error("expected ./services/api to match ./services/api/main.go")
	}
}

func TestIsPathChanged_EmptyPath(t *testing.T) {
	files := map[string]struct{}{
		"some/file.go": {},
	}

	if !isPathChanged(files, "") {
		t.Error("empty path with files should return true")
	}
	if !isPathChanged(files, "./") {
		t.Error("./ path with files should return true")
	}

	empty := map[string]struct{}{}
	if isPathChanged(empty, "") {
		t.Error("empty path with no files should return false")
	}
}

func TestIsPathChanged_DirectoryPrefix(t *testing.T) {
	files := map[string]struct{}{
		"infra/infra-1/main.tf": {},
		"apps/web/src/app.js":   {},
	}

	if !isPathChanged(files, "infra/infra-1") {
		t.Error("expected infra/infra-1 to match infra/infra-1/main.tf")
	}
	if !isPathChanged(files, "infra") {
		t.Error("expected infra to match infra/infra-1/main.tf")
	}
	if !isPathChanged(files, "apps/web") {
		t.Error("expected apps/web to match apps/web/src/app.js")
	}
	if isPathChanged(files, "deploy") {
		t.Error("expected deploy to not match any file")
	}
}

func TestIsPathChanged_ExactFile(t *testing.T) {
	files := map[string]struct{}{
		"infra/infra-1": {},
	}

	if !isPathChanged(files, "infra/infra-1") {
		t.Error("expected exact path match")
	}
}

func TestIsPathChanged_TrailingSlash(t *testing.T) {
	files := map[string]struct{}{
		"infra/infra-1/main.tf": {},
	}

	if !isPathChanged(files, "infra/infra-1/") {
		t.Error("expected trailing slash to be stripped and match")
	}
}

func TestIsFileChanged_DotSlashPrefix(t *testing.T) {
	files := map[string]struct{}{
		"services/api/component.yaml": {},
	}

	if !isFileChanged(files, "./services/api/component.yaml") {
		t.Error("expected ./services/api/component.yaml to match services/api/component.yaml")
	}
}

func TestIsFileChanged_EmptyTarget(t *testing.T) {
	files := map[string]struct{}{
		"some/file.go": {},
	}

	if isFileChanged(files, "") {
		t.Error("empty target should return false")
	}
}

func TestIsFileChanged_NoMatch(t *testing.T) {
	files := map[string]struct{}{
		"apps/web/src/app.js": {},
	}

	if isFileChanged(files, "intent.yaml") {
		t.Error("intent.yaml should not match apps/web/src/app.js")
	}
}

func TestIsFileChanged_ComponentManifestRequiresExactPath(t *testing.T) {
	files := map[string]struct{}{
		"website/component.yaml": {},
	}

	if isFileChanged(files, "apps/api-edge/component.yaml") {
		t.Error("component.yaml basename should not match a different component manifest path")
	}
	if !isFileChanged(files, "website/component.yaml") {
		t.Error("exact component manifest path should match")
	}
}

func TestIsIntentPathChanged_BasenameMatch(t *testing.T) {
	files := map[string]struct{}{
		"examples/intent.yaml": {},
	}

	if !isIntentPathChanged(files, "intent.yaml") {
		t.Error("expected intent path detection to use basename matching")
	}
	if !isIntentPathChanged(files, "examples/intent.yaml") {
		t.Error("expected exact match to work")
	}
	if isIntentPathChanged(files, "other.yaml") {
		t.Error("non-matching path should return false")
	}
}

func TestChangedFilter_PRScenario_SingleComponentChanged(t *testing.T) {
	changedFiles := map[string]struct{}{
		"website/component.yaml": {},
		"website/package.json":   {},
		".github/workflows/workflow.yml": {},
		".gitignore":                     {},
		"kiox.yaml":                      {},
	}

	type component struct {
		name       string
		sourcePath string
		path       string
	}

	components := []component{
		{name: "docs-site", sourcePath: "website/component.yaml", path: "website"},
		{name: "api-edge", sourcePath: "apps/api-edge/component.yaml", path: "apps/api-edge"},
		{name: "admin-console", sourcePath: "apps/admin-console/component.yaml", path: "apps/admin-console"},
		{name: "cluster-addons", sourcePath: "infra/cluster-addons/component.yaml", path: "infra/cluster-addons"},
		{name: "commerce-checkout", sourcePath: "charts/commerce-checkout/component.yaml", path: "charts/commerce-checkout"},
	}

	intentFile := "intent.yaml"
	intentChanged := isIntentPathChanged(changedFiles, intentFile)
	if intentChanged {
		t.Fatal("intent.yaml should not be detected as changed")
	}

	changedComps := make(map[string]bool)
	for _, comp := range components {
		if isFileChanged(changedFiles, comp.sourcePath) {
			changedComps[comp.name] = true
		} else if comp.path != "" && comp.path != "./" {
			if isPathChanged(changedFiles, comp.path) {
				changedComps[comp.name] = true
			}
		}
	}

	if !changedComps["docs-site"] {
		t.Error("docs-site should be detected as changed (website/component.yaml modified)")
	}
	if changedComps["api-edge"] {
		t.Error("api-edge should NOT be detected as changed")
	}
	if changedComps["admin-console"] {
		t.Error("admin-console should NOT be detected as changed")
	}
	if changedComps["cluster-addons"] {
		t.Error("cluster-addons should NOT be detected as changed")
	}
	if changedComps["commerce-checkout"] {
		t.Error("commerce-checkout should NOT be detected as changed")
	}
	if len(changedComps) != 1 {
		t.Errorf("expected exactly 1 changed component, got %d: %v", len(changedComps), changedComps)
	}
}

func TestChangedFilter_PRScenario_MultipleComponentsChanged(t *testing.T) {
	changedFiles := map[string]struct{}{
		"apps/api-edge/src/handler.ts":            {},
		"infra/cluster-addons/values.yaml":        {},
		"infra/cluster-addons/templates/rbac.yaml": {},
	}

	type component struct {
		name       string
		sourcePath string
		path       string
	}

	components := []component{
		{name: "docs-site", sourcePath: "website/component.yaml", path: "website"},
		{name: "api-edge", sourcePath: "apps/api-edge/component.yaml", path: "apps/api-edge"},
		{name: "cluster-addons", sourcePath: "infra/cluster-addons/component.yaml", path: "infra/cluster-addons"},
		{name: "commerce-checkout", sourcePath: "charts/commerce-checkout/component.yaml", path: "charts/commerce-checkout"},
	}

	changedComps := make(map[string]bool)
	for _, comp := range components {
		if isFileChanged(changedFiles, comp.sourcePath) {
			changedComps[comp.name] = true
		} else if comp.path != "" && comp.path != "./" {
			if isPathChanged(changedFiles, comp.path) {
				changedComps[comp.name] = true
			}
		}
	}

	if !changedComps["api-edge"] {
		t.Error("api-edge should be detected as changed (source files modified)")
	}
	if !changedComps["cluster-addons"] {
		t.Error("cluster-addons should be detected as changed (files under path modified)")
	}
	if changedComps["docs-site"] {
		t.Error("docs-site should NOT be detected as changed")
	}
	if changedComps["commerce-checkout"] {
		t.Error("commerce-checkout should NOT be detected as changed")
	}
	if len(changedComps) != 2 {
		t.Errorf("expected exactly 2 changed components, got %d: %v", len(changedComps), changedComps)
	}
}

func TestChangedFilter_PRScenario_IntentFileChanged(t *testing.T) {
	changedFiles := map[string]struct{}{
		"intent.yaml": {},
	}

	intentChanged := isIntentPathChanged(changedFiles, "intent.yaml")
	if !intentChanged {
		t.Error("intent.yaml should be detected as changed")
	}
}

func TestChangedFilter_PRScenario_OnlyRootFilesChanged(t *testing.T) {
	changedFiles := map[string]struct{}{
		".github/workflows/ci.yml": {},
		".gitignore":               {},
		"README.md":                {},
	}

	type component struct {
		name       string
		sourcePath string
		path       string
	}

	components := []component{
		{name: "docs-site", sourcePath: "website/component.yaml", path: "website"},
		{name: "api-edge", sourcePath: "apps/api-edge/component.yaml", path: "apps/api-edge"},
	}

	intentChanged := isIntentPathChanged(changedFiles, "intent.yaml")
	if intentChanged {
		t.Error("intent.yaml should NOT be detected as changed")
	}

	changedComps := make(map[string]bool)
	for _, comp := range components {
		if isFileChanged(changedFiles, comp.sourcePath) {
			changedComps[comp.name] = true
		} else if comp.path != "" && comp.path != "./" {
			if isPathChanged(changedFiles, comp.path) {
				changedComps[comp.name] = true
			}
		}
	}

	if len(changedComps) != 0 {
		t.Errorf("no components should be detected as changed, got %d: %v", len(changedComps), changedComps)
	}
}

func TestChangedFilter_ComponentManifestChange_DoesNotMatchOtherComponents(t *testing.T) {
	changedFiles := map[string]struct{}{
		"apps/api-edge/component.yaml": {},
	}

	type component struct {
		name       string
		sourcePath string
	}

	components := []component{
		{name: "api-edge", sourcePath: "apps/api-edge/component.yaml"},
		{name: "admin-console", sourcePath: "apps/admin-console/component.yaml"},
		{name: "cluster-addons", sourcePath: "infra/cluster-addons/component.yaml"},
		{name: "docs-site", sourcePath: "website/component.yaml"},
	}

	changedComps := make(map[string]bool)
	for _, comp := range components {
		if isFileChanged(changedFiles, comp.sourcePath) {
			changedComps[comp.name] = true
		}
	}

	if !changedComps["api-edge"] {
		t.Error("api-edge should be detected as changed (its component.yaml was modified)")
	}
	if len(changedComps) != 1 {
		t.Errorf("only api-edge should match, but got %d components: %v", len(changedComps), changedComps)
	}
}

func TestNormalizeFilePath_BackslashHandling(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"infra\\infra-1\\main.tf", "infra/infra-1/main.tf"},
		{"infra/infra-1/", "infra/infra-1"},
		{"infra/infra-1", "infra/infra-1"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeFilePath(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeFilePath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFilepathBase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"infra/infra-1/main.tf", "main.tf"},
		{"intent.yaml", "intent.yaml"},
		{"a/b/c", "c"},
	}

	for _, tt := range tests {
		result := filepathBase(tt.input)
		if result != tt.expected {
			t.Errorf("filepathBase(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
