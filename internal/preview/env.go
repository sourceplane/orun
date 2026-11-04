package preview

import (
	"sort"
	"strconv"
	"strings"
)

// A dev server runs the repository's code, so it never gets the box's
// secrets (PB-12): the session's credential, the model and forge tokens, and
// anything named like a secret. orun's redaction (internal/redact) works on
// values, not names, so the name policy is here.

// secretNames are stripped by exact name.
var secretNames = map[string]bool{
	"ORUN_SESSION_TOKEN": true,
	"ORUN_TOKEN":         true,
	"ORUN_TOKEN_FILE":    true,
	"GITHUB_TOKEN":       true,
}

// secretPrefixes and secretSuffixes are stripped by shape.
var (
	secretPrefixes = []string{"ANTHROPIC_", "OPENROUTER_", "GH_"}
	secretSuffixes = []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD", "_CREDENTIALS"}
)

// IsSecretEnvName reports whether an inherited variable is kept from a
// preview's processes. Case-insensitive.
func IsSecretEnvName(name string) bool {
	n := strings.ToUpper(name)
	if secretNames[n] {
		return true
	}
	for _, p := range secretPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	for _, s := range secretSuffixes {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}

// ChildEnv is a preview process's environment: the inherited one without its
// secrets, PORT set to the component's port, then the component's own
// spec.preview.env (which wins, PORT included).
func ChildEnv(environ []string, own map[string]string, port int) []string {
	out := make([]string, 0, len(environ)+len(own)+1)
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || IsSecretEnvName(name) || name == "PORT" {
			continue
		}
		if _, overridden := own[name]; overridden {
			continue
		}
		out = append(out, kv)
	}
	if _, set := own["PORT"]; !set && port > 0 {
		out = append(out, "PORT="+strconv.Itoa(port))
	}
	keys := make([]string, 0, len(own))
	for k := range own {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+own[k])
	}
	return out
}
