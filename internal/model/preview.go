package model

// The live preview a repository declares (orun-cloud saas-parallel-baselining
// PB-11): each component says how it runs itself (spec.preview), and the
// intent names the one to show (preview.component). `orun preview` runs it;
// `orun agent serve` keeps it running for a coding session.

// Preview port bounds: unprivileged ports only, as the platform forwards.
const (
	PreviewPortMin     = 1024
	PreviewPortMax     = 65535
	DefaultPreviewPath = "/"
)

// IntentPreview is the intent's top-level `preview:` block.
type IntentPreview struct {
	// Component names the component whose preview is shown.
	Component string `yaml:"component" json:"component"`
}

// ComponentPreview is a component's spec.preview: how it runs its dev server.
type ComponentPreview struct {
	// Run is the argv, run from the component's directory.
	Run []string `yaml:"run" json:"run"`
	// Port is where the server listens on 127.0.0.1 (1024–65535).
	Port int `yaml:"port" json:"port"`
	// Needs are components whose own previews start first.
	Needs []string `yaml:"needs,omitempty" json:"needs,omitempty"`
	// Path is the page the preview opens (default /).
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Env is plain, non-secret env for the server.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// PreviewProblems checks a spec.preview block's own fields; cross-component
// rules (needs, the intent's choice) are the preview package's. Messages name
// the field, relative to spec.preview.
func PreviewProblems(run []string, port int, path string) []string {
	var out []string
	if len(run) == 0 {
		out = append(out, "run must be a non-empty argv list")
	} else if run[0] == "" {
		out = append(out, "run[0] (the program) is empty")
	}
	if port < PreviewPortMin || port > PreviewPortMax {
		out = append(out, "port must be an integer from 1024 to 65535")
	}
	if path != "" && path[0] != '/' {
		out = append(out, "path must start with /")
	}
	return out
}
