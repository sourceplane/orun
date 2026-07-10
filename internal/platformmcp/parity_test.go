package platformmcp

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
)

// The anti-drift contract (design §4): the vendored manifest exported from
// the TS plane pins this package's roster. These tests fail on any name /
// description / schema / annotation drift — over the full 25-tool roster
// since UM2 (reads and writes alike).

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (go.mod)")
		}
		dir = parent
	}
}

func vendoredManifest(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), "specs", "orun-cloud", "vendored", "mcp-tool-manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vendored manifest: %v", err)
	}
	return data
}

// TestEmbeddedManifestMatchesVendored pins the go:embed copy in this package
// to the vendored contract byte-for-byte (embed cannot reach specs/ from
// here; the copy must be re-copied on every re-vendor).
func TestEmbeddedManifestMatchesVendored(t *testing.T) {
	if !bytes.Equal(vendoredManifest(t), manifestJSON) {
		t.Fatal("internal/platformmcp/mcp-tool-manifest.json differs from " +
			"specs/orun-cloud/vendored/mcp-tool-manifest.json; re-copy the vendored file " +
			"into the package (cp specs/orun-cloud/vendored/mcp-tool-manifest.json internal/platformmcp/)")
	}
}

// TestVendoredManifestChecksum is the drift guard for the vendored tool
// manifest, mirroring TestVendoredContractChecksum (OC0 pattern): the
// vendored file must match the sha256 recorded in the sibling CHECKSUM.
func TestVendoredManifestChecksum(t *testing.T) {
	sum := sha256.Sum256(vendoredManifest(t))
	got := hex.EncodeToString(sum[:])

	f, err := os.Open(filepath.Join(repoRoot(t), "specs", "orun-cloud", "vendored", "CHECKSUM"))
	if err != nil {
		t.Fatalf("open CHECKSUM: %v", err)
	}
	defer f.Close()
	want := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "mcp-tool-manifest.json" {
			want = fields[0]
		}
	}
	if want == "" {
		t.Fatal("specs/orun-cloud/vendored/CHECKSUM has no entry for mcp-tool-manifest.json")
	}
	if got != want {
		t.Fatalf("vendored mcp-tool-manifest.json drifted from its recorded checksum:\n"+
			"  recorded (CHECKSUM): %s\n  actual   (file):     %s\n"+
			"re-vendor from orun-cloud (MCP9 export) then update CHECKSUM and the embedded copy.", want, got)
	}
}

// canon renders a decoded JSON value canonically (json.Marshal sorts object
// keys), normalizing both sides of a schema comparison the same way.
func canon(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canon: %v", err)
	}
	return string(b)
}

// TestManifestParity asserts the advertised roster equals the vendored
// manifest, tool for tool: count, order, names, descriptions, titles,
// annotations, and normalized inputSchemas — the full 25 (UM2), and the
// readOnlyHint:true 19 under ReadOnly.
func TestManifestParity(t *testing.T) {
	var m manifest
	if err := json.Unmarshal(vendoredManifest(t), &m); err != nil {
		t.Fatalf("parse vendored manifest: %v", err)
	}
	if m.ToolCount != len(m.Tools) {
		t.Fatalf("manifest toolCount %d != %d tools", m.ToolCount, len(m.Tools))
	}
	if len(m.Tools) != 25 {
		t.Fatalf("UM2 expects 25 tools, manifest carries %d", len(m.Tools))
	}

	var wantReads []manifestTool
	for _, tool := range m.Tools {
		if ro, _ := tool.Annotations["readOnlyHint"].(bool); ro {
			wantReads = append(wantReads, tool)
		}
	}
	if len(wantReads) != m.ReadOnlyToolCount {
		t.Fatalf("manifest readOnlyToolCount %d but %d readOnlyHint:true tools", m.ReadOnlyToolCount, len(wantReads))
	}
	if len(wantReads) != 19 {
		t.Fatalf("UM2 expects 19 read tools, manifest carries %d", len(wantReads))
	}

	for _, tc := range []struct {
		mode string
		got  []mcpserve.ToolDef
		want []manifestTool
	}{
		// no default workspace: schemas must be the manifest's, verbatim
		{"full", (&Provider{}).Tools(), m.Tools},
		{"read-only", (&Provider{ReadOnly: true}).Tools(), wantReads},
	} {
		if len(tc.got) != len(tc.want) {
			t.Fatalf("%s: Provider.Tools() = %d tools, want %d", tc.mode, len(tc.got), len(tc.want))
		}
		for i, w := range tc.want {
			g := tc.got[i]
			if g.Name != w.Name {
				t.Fatalf("%s tool %d: name %q, want %q (order must match the manifest)", tc.mode, i, g.Name, w.Name)
			}
			if g.Description != w.Description {
				t.Errorf("%s: description drifted from the manifest", w.Name)
			}
			if g.Title != w.Title {
				t.Errorf("%s: title = %q, want %q", w.Name, g.Title, w.Title)
			}
			if canon(t, g.Annotations) != canon(t, w.Annotations) {
				t.Errorf("%s: annotations = %s, want %s", w.Name, canon(t, g.Annotations), canon(t, w.Annotations))
			}
			var wantSchema interface{}
			if err := json.Unmarshal(w.InputSchema, &wantSchema); err != nil {
				t.Fatalf("%s: manifest schema: %v", w.Name, err)
			}
			if canon(t, g.InputSchema) != canon(t, wantSchema) {
				t.Errorf("%s: inputSchema drifted:\n got  %s\n want %s", w.Name, canon(t, g.InputSchema), canon(t, wantSchema))
			}
		}
	}
}

// TestWorkspaceDefaultAdjustsSchema: with an ambient default, `workspace`
// drops out of each schema's required array (and only that — properties are
// untouched); tools without a workspace argument are unchanged.
func TestWorkspaceDefaultAdjustsSchema(t *testing.T) {
	tools := (&Provider{DefaultWorkspace: "ws_ambient"}).Tools()
	for _, tool := range tools {
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		_, hasWorkspace := props["workspace"]
		req, _ := tool.InputSchema["required"].([]interface{})
		for _, r := range req {
			if r == "workspace" {
				t.Errorf("%s: workspace still required despite an active default", tool.Name)
			}
		}
		if !hasWorkspace && len(req) == 0 {
			switch tool.Name {
			case "whoami", "workspaces_list", "security_events_list":
			default:
				t.Errorf("%s: unexpectedly has neither a workspace property nor required args", tool.Name)
			}
		}
	}
	// projects_list requires only workspace in the manifest; with the default
	// active its required key must vanish entirely.
	for _, tool := range tools {
		if tool.Name == "projects_list" {
			if _, ok := tool.InputSchema["required"]; ok {
				t.Errorf("projects_list: required = %v, want the key dropped", tool.InputSchema["required"])
			}
		}
		if tool.Name == "catalog_get_entity" {
			req, _ := tool.InputSchema["required"].([]interface{})
			if canon(t, req) != `["entityRef"]` {
				t.Errorf("catalog_get_entity: required = %s, want [\"entityRef\"]", canon(t, req))
			}
		}
	}
}
