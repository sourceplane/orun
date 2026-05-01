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

func TestIsFileChanged_BasenameMatch(t *testing.T) {
	files := map[string]struct{}{
		"examples/intent.yaml": {},
	}

	if !isFileChanged(files, "intent.yaml") {
		t.Error("expected basename intent.yaml to match examples/intent.yaml")
	}
	if !isFileChanged(files, "examples/intent.yaml") {
		t.Error("expected exact path match")
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

func TestIsIntentPathChanged_DelegatesToFileChanged(t *testing.T) {
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
