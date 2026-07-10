// Package platformmcp is the platform tool plane of the orun MCP
// (orun-mcp UM1): the 19 read tools of the shipped TS plane
// (orun-cloud/packages/mcp), reimplemented natively over the public API via
// internal/remotestate. The stdio transport lives in internal/mcpserve; this
// package supplies the tools.
//
// Tool names, descriptions, input schemas, and annotations are EMBEDDED from
// the vendored contract (specs/orun-cloud/vendored/mcp-tool-manifest.json —
// design §4: the TS plane is the source of truth). mcp-tool-manifest.json in
// this directory is a byte-for-byte copy for go:embed (embed cannot reach
// outside the package dir); parity_test.go pins the copy to the vendored file
// and the advertised roster to the manifest, so schema parity holds by
// construction and drift is a test failure.
package platformmcp

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed mcp-tool-manifest.json
var manifestJSON []byte

// manifestTool is one tool entry of the vendored manifest.
type manifestTool struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	InputSchema json.RawMessage        `json:"inputSchema"`
	Annotations map[string]interface{} `json:"annotations"`
}

type manifest struct {
	ManifestVersion   int            `json:"manifestVersion"`
	ReadOnlyToolCount int            `json:"readOnlyToolCount"`
	ToolCount         int            `json:"toolCount"`
	Tools             []manifestTool `json:"tools"`
}

// toolSpec is one advertised tool: the manifest entry plus what the dispatch
// layer needs (workspace-argument shape for the default fill).
type toolSpec struct {
	manifestTool
	hasWorkspace      bool
	workspaceRequired bool
}

// readTools is the plane's roster, in manifest order, filtered to
// readOnlyHint:true (UM1 — the 6 writes are UM2's).
var readTools = mustLoadReadTools()

var toolsByName = func() map[string]*toolSpec {
	m := make(map[string]*toolSpec, len(readTools))
	for i := range readTools {
		m[readTools[i].Name] = &readTools[i]
	}
	return m
}()

func mustLoadReadTools() []toolSpec {
	var m manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		panic(fmt.Sprintf("platformmcp: embedded manifest is invalid: %v", err))
	}
	var out []toolSpec
	for _, t := range m.Tools {
		if ro, _ := t.Annotations["readOnlyHint"].(bool); !ro {
			continue
		}
		spec := toolSpec{manifestTool: t}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			panic(fmt.Sprintf("platformmcp: tool %s has an invalid inputSchema: %v", t.Name, err))
		}
		_, spec.hasWorkspace = schema.Properties["workspace"]
		for _, r := range schema.Required {
			if r == "workspace" {
				spec.workspaceRequired = true
			}
		}
		out = append(out, spec)
	}
	if len(out) != m.ReadOnlyToolCount {
		panic(fmt.Sprintf("platformmcp: embedded manifest advertises %d read tools but carries %d", m.ReadOnlyToolCount, len(out)))
	}
	return out
}

// decodeSchema re-unmarshals a tool's schema into a fresh mutable map (the
// advertised copy may be adjusted per provider; the manifest form never is).
func decodeSchema(raw json.RawMessage) map[string]interface{} {
	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic(fmt.Sprintf("platformmcp: schema decode: %v", err))
	}
	return schema
}

// dropRequired removes name from the schema's required array (deleting the
// key when it empties) — MCP1's applyWorkspaceDefault semantics: an active
// ambient default makes `workspace` optional on the advertised schema.
func dropRequired(schema map[string]interface{}, name string) {
	req, ok := schema["required"].([]interface{})
	if !ok {
		return
	}
	out := make([]interface{}, 0, len(req))
	for _, r := range req {
		if s, ok := r.(string); ok && s == name {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		delete(schema, "required")
	} else {
		schema["required"] = out
	}
}
