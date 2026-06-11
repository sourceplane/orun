package main

// catalog_read_test.go is the C5 PR-2 read-surface E2E suite: seed a real git
// workspace, `refresh` it, then exercise every read subcommand against the
// persisted snapshot in both text and --json modes. The harness reuses the
// PR-1 helpers (seedGitCatalogWorkspace, withTempIntentRoot, captureStdout,
// resetCatalogFlags) so the read tests run against exactly what refresh writes.
//
// Coverage:
//   - list:     one component row, JSON envelope kind + componentKey.
//   - describe: §4 sections (text) + manifest round-trip (JSON); missing
//     component exits 6.
//   - tree:     dependency graph render + JSON {nodes,edges}; bad --direction
//     exits 1.
//   - history:  empty history (no executions yet in C5) exits 0.
//   - validate: clean workspace is valid, exit 0.
//   - diff:     real engine — see catalog_diff_test.go.

import (
	"encoding/json"
	"strings"
	"testing"
)

// refreshSeededCatalog plants a git workspace and refreshes it so the read
// subcommands have a persisted snapshot to read. Returns nothing; the catalog
// lives under the test's temp intent root.
func refreshSeededCatalog(t *testing.T) {
	t.Helper()
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)
	catalogJSONFlag = true
	_ = captureStdout(t, func() error { return runCatalogRefresh(nil) })
	// Leave JSON off by default; each subtest opts in.
	catalogJSONFlag = false
}

func TestCatalogList_E2E(t *testing.T) {
	refreshSeededCatalog(t)

	// Text mode: the component name appears.
	out := captureStdout(t, func() error { return runCatalogList(nil) })
	if !strings.Contains(out, "svc-a") {
		t.Errorf("list text missing component, got:\n%s", out)
	}

	// JSON mode: envelope kind + one row.
	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	outJSON := captureStdout(t, func() error { return runCatalogList(nil) })
	var env catalogEnvelope
	var rows []catalogListRow
	env.Data = &rows
	if err := json.Unmarshal([]byte(outJSON), &env); err != nil {
		t.Fatalf("list envelope: %v\n%s", err, outJSON)
	}
	if env.Kind != kindCatalogListResult {
		t.Errorf("kind = %q, want %q", env.Kind, kindCatalogListResult)
	}
	if len(rows) != 1 || rows[0].Name != "svc-a" {
		t.Fatalf("expected one svc-a row, got %+v", rows)
	}
	if rows[0].ComponentKey == "" || rows[0].CatalogSnapshotKey == "" {
		t.Errorf("row missing keys: %+v", rows[0])
	}
}

func TestCatalogList_E2E_OwnerFilter(t *testing.T) {
	refreshSeededCatalog(t)

	// A non-matching owner filter yields no rows; the catalog still resolves
	// (exit 0, empty listing) rather than erroring.
	prev := catalogListOwnerFlag
	catalogListOwnerFlag = "team/does-not-exist"
	t.Cleanup(func() { catalogListOwnerFlag = prev })

	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	outJSON := captureStdout(t, func() error { return runCatalogList(nil) })
	var env catalogEnvelope
	var rows []catalogListRow
	env.Data = &rows
	if err := json.Unmarshal([]byte(outJSON), &env); err != nil {
		t.Fatalf("list envelope: %v\n%s", err, outJSON)
	}
	if len(rows) != 0 {
		t.Errorf("owner filter should exclude svc-a, got %+v", rows)
	}
}

func TestCatalogDescribe_E2E(t *testing.T) {
	refreshSeededCatalog(t)

	// Text mode: §4 sections present.
	out := captureStdout(t, func() error { return runCatalogDescribe(nil, "svc-a") })
	for _, want := range []string{"Component", "Ownership", "Source", "Dependencies"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe text missing section %q, got:\n%s", want, out)
		}
	}

	// JSON mode: manifest round-trips.
	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	outJSON := captureStdout(t, func() error { return runCatalogDescribe(nil, "svc-a") })
	var env catalogEnvelope
	var data catalogDescribeData
	env.Data = &data
	if err := json.Unmarshal([]byte(outJSON), &env); err != nil {
		t.Fatalf("describe envelope: %v\n%s", err, outJSON)
	}
	if env.Kind != kindCatalogDescribeResult {
		t.Errorf("kind = %q", env.Kind)
	}
	if data.Manifest.Identity.Name != "svc-a" {
		t.Errorf("manifest name = %q", data.Manifest.Identity.Name)
	}
	// The typed Spec/Metadata are reconstructed by round-tripping the object-
	// model component's generic maps; assert the seeded values survive.
	if data.Manifest.Spec.Type != "service" {
		t.Errorf("manifest spec.type = %q, want service", data.Manifest.Spec.Type)
	}
	if data.Manifest.Spec.System != "payments" {
		t.Errorf("manifest spec.system = %q, want payments", data.Manifest.Spec.System)
	}
	if data.Manifest.Metadata.Owner != "group:team/x" {
		t.Errorf("manifest metadata.owner = %q, want group:team/x", data.Manifest.Metadata.Owner)
	}
	if data.Manifest.Source.SourceSnapshotKey == "" || data.Manifest.Source.CatalogSnapshotKey == "" {
		t.Errorf("manifest source keys empty: %+v", data.Manifest.Source)
	}
	if data.Executions == nil {
		t.Error("executions must be a non-nil array even when empty")
	}
}

func TestCatalogDescribe_E2E_MissingExit6(t *testing.T) {
	refreshSeededCatalog(t)

	err := runCatalogDescribe(nil, "ghost")
	if err == nil {
		t.Fatal("expected error for unknown component")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 6 {
		t.Errorf("expected exit 6 for missing component, got %v", err)
	}
}

func TestCatalogTree_E2E(t *testing.T) {
	refreshSeededCatalog(t)

	// Text mode renders without error (single-component graph).
	out := captureStdout(t, func() error { return runCatalogTree(nil, "") })
	if strings.TrimSpace(out) == "" {
		t.Error("tree text should not be empty")
	}

	// JSON mode: {nodes, edges} envelope.
	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	outJSON := captureStdout(t, func() error { return runCatalogTree(nil, "") })
	var env catalogEnvelope
	var data catalogTreeData
	env.Data = &data
	if err := json.Unmarshal([]byte(outJSON), &env); err != nil {
		t.Fatalf("tree envelope: %v\n%s", err, outJSON)
	}
	if env.Kind != kindCatalogTreeResult {
		t.Errorf("kind = %q", env.Kind)
	}
	// svc-a has no dependencies, so at least its own node should be present.
	if len(data.Nodes) < 1 {
		t.Errorf("expected at least one node, got %+v", data.Nodes)
	}
}

func TestCatalogTree_E2E_BadDirectionExit1(t *testing.T) {
	refreshSeededCatalog(t)

	prev := catalogTreeDirectionFlag
	catalogTreeDirectionFlag = "sideways"
	t.Cleanup(func() { catalogTreeDirectionFlag = prev })

	err := runCatalogTree(nil, "")
	if err == nil {
		t.Fatal("expected error for bad --direction")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("expected exit 1 for bad direction, got %v", err)
	}
}

func TestCatalogHistory_E2E_EmptyExit0(t *testing.T) {
	refreshSeededCatalog(t)

	// No executions are written in C5, so history is empty but exits 0.
	out := captureStdout(t, func() error { return runCatalogHistory(nil, "svc-a") })
	if !strings.Contains(out, "No executions") {
		t.Errorf("expected empty-history line, got:\n%s", out)
	}

	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	outJSON := captureStdout(t, func() error { return runCatalogHistory(nil, "svc-a") })
	var env catalogEnvelope
	var rows []map[string]any
	env.Data = &rows
	if err := json.Unmarshal([]byte(outJSON), &env); err != nil {
		t.Fatalf("history envelope: %v\n%s", err, outJSON)
	}
	if env.Kind != kindCatalogHistoryResult {
		t.Errorf("kind = %q", env.Kind)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty history, got %+v", rows)
	}
}

func TestCatalogValidate_E2E_CleanExit0(t *testing.T) {
	refreshSeededCatalog(t)

	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	out := captureStdout(t, func() error { return runCatalogValidate(nil) })
	var env catalogEnvelope
	var data catalogValidateData
	env.Data = &data
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("validate envelope: %v\n%s", err, out)
	}
	if env.Kind != kindCatalogValidateResult {
		t.Errorf("kind = %q", env.Kind)
	}
	if !data.Valid || data.Errors != 0 {
		t.Errorf("clean workspace should be valid, got %+v", data)
	}
}
