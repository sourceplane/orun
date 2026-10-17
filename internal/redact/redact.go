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
	"unicode/utf8"
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

// MaxLen is the length in bytes of the longest registered value or encoding;
// 0 when nothing is registered.
func (r *Redactor) MaxLen() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.patterns) == 0 {
		return 0
	}
	return len(r.patterns[0]) // longest-first
}

// SafeCut is where s may be split for Filter to mask each side exactly as it
// masks s whole: the largest offset at or before `at` that no occurrence of a
// registered value spans and that starts a UTF-8 rune. A stream of output
// redacted piece by piece (saas-live-runs LR8) cuts its pieces here, so a
// secret is never sent half in one piece and half in the next — where
// neither half would match it.
//
// It does not see bytes past the end of s: a caller holding a stream back
// keeps the last MaxLen()-1 bytes unsent, so an occurrence still arriving
// lies wholly in what it keeps.
func (r *Redactor) SafeCut(s string, at int) int {
	if at > len(s) {
		at = len(s)
	}
	if at < 0 {
		at = 0
	}
	var patterns []string
	if r != nil {
		r.mu.RLock()
		patterns = r.patterns
		r.mu.RUnlock()
	}
	for {
		for at > 0 && at < len(s) && !utf8.RuneStart(s[at]) {
			at--
		}
		moved := false
		for _, p := range patterns {
			// An occurrence spanning `at` starts in (at-len(p), at).
			lo := at - len(p) + 1
			if lo < 0 {
				lo = 0
			}
			hi := at + len(p) - 1
			if hi > len(s) {
				hi = len(s)
			}
			if hi-lo < len(p) {
				continue
			}
			window := s[lo:hi]
			for off := 0; off <= len(window)-len(p); {
				i := strings.Index(window[off:], p)
				if i < 0 {
					break
				}
				start := lo + off + i
				if start < at && start+len(p) > at {
					at = start
					moved = true
					break
				}
				off += i + 1
			}
			if moved {
				break
			}
		}
		if !moved {
			return at
		}
	}
}
