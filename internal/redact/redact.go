// Package redact masks resolved secret values in step output before it
// reaches any sink — console, live tail, remote log chunks, or the sealed
// content-addressed blob (specs/orun-secrets/runner-integration.md §4,
// Invariant 5). The redactor is per-run, seeded at resolve time (before any
// step that could echo a value runs), and discarded at seal.
package redact

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Mask is what a registered value (and its encodings) is replaced with.
const Mask = "***"

// minLength guards against ***-flooding: values shorter than this are not
// masked (the policy layer forbids trivially short secrets instead).
const minLength = 4

// Redactor replaces registered secret values — and their base64, URL-encoded,
// and JSON-escaped forms — with Mask. Safe for concurrent use: jobs run in
// parallel and register/filter from multiple goroutines.
type Redactor struct {
	mu       sync.RWMutex
	patterns []string
	replacer *strings.Replacer
}

// New returns an empty redactor; Filter is a no-op until values are added.
func New() *Redactor {
	return &Redactor{}
}

// Add registers secret values and their common encodings. Values shorter
// than 4 characters are ignored.
func (r *Redactor) Add(values ...string) {
	forms := make([]string, 0, len(values)*4)
	for _, v := range values {
		if len(v) < minLength {
			continue
		}
		forms = append(forms, v)
		forms = append(forms, base64.StdEncoding.EncodeToString([]byte(v)))
		if escaped := url.QueryEscape(v); escaped != v {
			forms = append(forms, escaped)
		}
		if raw, err := json.Marshal(v); err == nil {
			if quoted := string(raw[1 : len(raw)-1]); quoted != v {
				forms = append(forms, quoted)
			}
		}
	}
	if len(forms) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{}, len(r.patterns)+len(forms))
	for _, p := range r.patterns {
		seen[p] = struct{}{}
	}
	for _, f := range forms {
		if _, dup := seen[f]; !dup && len(f) >= minLength {
			r.patterns = append(r.patterns, f)
			seen[f] = struct{}{}
		}
	}
	// Longest-first so a value that contains another value's encoding is
	// masked whole rather than partially.
	sort.Slice(r.patterns, func(i, j int) bool { return len(r.patterns[i]) > len(r.patterns[j]) })
	pairs := make([]string, 0, len(r.patterns)*2)
	for _, p := range r.patterns {
		pairs = append(pairs, p, Mask)
	}
	r.replacer = strings.NewReplacer(pairs...)
}

// Filter returns s with every registered value (and encoding) masked.
// Nil-safe and empty-safe: with nothing registered, s is returned unchanged.
func (r *Redactor) Filter(s string) string {
	if r == nil {
		return s
	}
	r.mu.RLock()
	rep := r.replacer
	r.mu.RUnlock()
	if rep == nil {
		return s
	}
	return rep.Replace(s)
}
