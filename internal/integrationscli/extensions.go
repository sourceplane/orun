package integrationscli

import (
	"sync"

	"github.com/spf13/cobra"
)

// The native-extension seam (design.md §5): Go-registered subcommands may
// EXTEND a provider namespace with local behavior (recipe printers, verify
// helpers, file emitters) but never contradict it — on a path collision the
// served verb wins, the extension is not mounted, and the shadowing is logged
// at debug. Extensions that need cloud data go through configsurface/
// remotestate like everything else; provider SDKs stay forbidden here (see
// arch_test.go).

var (
	extensionsMu sync.Mutex
	extensions   = map[string][]*cobra.Command{}
)

// RegisterExtension registers cmd as a native extension under provider's
// namespace. Called from cmd/orun wiring (the materialize-Registry injection
// style); mounted after served verbs when the provider's tree renders.
func RegisterExtension(provider string, cmd *cobra.Command) {
	if provider == "" || cmd == nil {
		return
	}
	extensionsMu.Lock()
	defer extensionsMu.Unlock()
	extensions[provider] = append(extensions[provider], cmd)
}

// resetExtensions clears the registry — test helper only.
func resetExtensions() {
	extensionsMu.Lock()
	defer extensionsMu.Unlock()
	extensions = map[string][]*cobra.Command{}
}

// mountExtensions attaches provider's registered extensions after the served/
// derived verbs. Served wins on a name collision: the extension is skipped and
// the shadowing named at debug — server truth is never silently overridden.
func mountExtensions(providerCmd *cobra.Command, provider string, debugf func(string, ...interface{})) {
	extensionsMu.Lock()
	exts := append([]*cobra.Command(nil), extensions[provider]...)
	extensionsMu.Unlock()
	for _, ext := range exts {
		if shadowedBy(providerCmd, ext.Name()) {
			debugf("integrations: native extension %q on provider %q is shadowed by a served verb and was not mounted", ext.Name(), provider)
			continue
		}
		providerCmd.AddCommand(ext)
	}
}

func shadowedBy(parent *cobra.Command, name string) bool {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return true
		}
	}
	return false
}
