package actions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
)

// A connection reference names ONE of a workspace's connections when a
// provider has several (saas-multi-connections MCX-D7: an account may hold
// two Cloudflare accounts, two GitHub orgs, two Supabase orgs). It matches,
// in order of precedence, the connection id (`int_…`), its display name, or
// the provider account's login — the last two case-insensitively, because
// they are what a person sees in the console.
//
// The reference is optional everywhere it is accepted. Without one, every
// action keeps exactly what it did before references existed.

// matchConnectionRef returns the active connections of `provider` (any
// provider when empty) that `ref` names. An id match wins outright; otherwise
// every display-name or login match is returned, so a caller can refuse an
// ambiguous name instead of guessing.
func matchConnectionRef(conns []configsurface.Connection, provider, ref string) []configsurface.Connection {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	var byName []configsurface.Connection
	for _, c := range conns {
		if !strings.EqualFold(c.Status, "active") {
			continue
		}
		if provider != "" && !strings.EqualFold(c.Provider, provider) {
			continue
		}
		if c.ID == ref {
			return []configsurface.Connection{c}
		}
		if (c.DisplayName != nil && strings.EqualFold(strings.TrimSpace(*c.DisplayName), ref)) ||
			(c.ExternalAccountLogin != nil && strings.EqualFold(strings.TrimSpace(*c.ExternalAccountLogin), ref)) {
			byName = append(byName, c)
		}
	}
	return byName
}

// connectionCandidates lists the active connections of `provider` as
// "label (id)" — what an error offers a blueprint author to pin.
func connectionCandidates(conns []configsurface.Connection, provider string) string {
	var out []string
	for _, c := range conns {
		if strings.EqualFold(c.Status, "active") && strings.EqualFold(c.Provider, provider) {
			out = append(out, fmt.Sprintf("%s (%s)", c.AccountLabel(), c.ID))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}
