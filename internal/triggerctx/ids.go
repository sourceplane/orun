package triggerctx

import (
	"crypto/rand"
	"fmt"
	"math"
	mathrand "math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idPrefixTrigger is concatenated with the underlying ULID to form a
// TriggerOccurrence.TriggerID. The same shape is reused by sibling packages
// (`rev_…`, `exec_…`) — see data-model.md §10.
const idPrefixTrigger = "trg_"

// triggerKeyPattern matches the rendered form of TriggerKey.
//
//	trg-<normalizedScope>-(<7-hex>|local-dirty|no-git)
//
// See data-model.md §2.2.
var triggerKeyPattern = regexp.MustCompile(`^trg-[a-z0-9-]+-([a-f0-9]{7}|local-dirty|no-git)$`)

// scopeReplacer collapses characters disallowed in a scope segment into '-'.
// We intentionally drop anything outside [a-z0-9] so the resulting key is safe
// in both filesystem path segments and object-store keys.
var scopeSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// monotonicEntropy is a process-shared monotonic entropy source for ULID
// generation. data-model.md §10 requires a *single* shared source per process
// so IDs are sort-stable within the same millisecond across goroutines.
//
// We seed from crypto/rand at init time and wrap with a Mutex because
// ulid.MonotonicEntropy.MonotonicRead mutates state (it must be exclusive).
var (
	monoMu      sync.Mutex
	monoEntropy *ulid.MonotonicEntropy
)

func init() {
	seed := readCryptoSeed()
	monoEntropy = ulid.Monotonic(mathrand.New(mathrand.NewSource(seed)), 0)
}

func readCryptoSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail on a supported platform; fall back to
		// the current nanosecond timestamp so the package remains usable.
		return time.Now().UnixNano()
	}
	var v int64
	for i := 0; i < 8; i++ {
		v = (v << 8) | int64(b[i])
	}
	if v == math.MinInt64 {
		v = 0 // avoid the one value that has no positive counterpart
	}
	return v
}

// NewTriggerID returns a new monotonic ULID prefixed with "trg_". The returned
// identifier is globally unique and lexically sortable by creation time.
func NewTriggerID() string {
	return idPrefixTrigger + newULID(time.Now()).String()
}

// newULIDAt is exposed for tests so they can pin the timestamp; production
// callers should use NewTriggerID.
func newULID(t time.Time) ulid.ULID {
	monoMu.Lock()
	defer monoMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), monoEntropy)
}

// TriggerKey deterministically renders the human-meaningful key folder used
// under .orun/refs/triggers and embedded in revision keys. The key is a pure
// function of TriggerSource.SourceScope and TriggerSource.HeadRevision, so
// identical inputs always produce identical output (the property under test
// in test-plan.md §3.1).
//
// Format: trg-<normalizedScope>-<shortSha|local-dirty|no-git>
func TriggerKey(t TriggerOccurrence) string {
	scope := normalizeScope(t.Source.SourceScope)
	if scope == "" {
		scope = "unknown"
	}
	marker := worktreeMarker(t.Source)
	return fmt.Sprintf("trg-%s-%s", scope, marker)
}

// TriggerKeyPattern returns the regexp every well-formed TriggerKey must
// satisfy. Exposed so downstream packages (statestore, revision) can validate
// keys they read from disk without re-deriving the rule.
func TriggerKeyPattern() *regexp.Regexp { return triggerKeyPattern }

// normalizeScope lower-cases the input, replaces any character outside
// [a-z0-9-] with '-', and collapses runs of '-' so the result is a safe
// segment for both filesystem and object-store paths.
func normalizeScope(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = scopeSanitizer.ReplaceAllString(s, "-")
	// collapse runs of '-'
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}

// shortSHA returns the first 7 characters of a hex SHA, lower-cased. Returns
// the empty string if the input is not at least 7 hex digits.
func shortSHA(sha string) string {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if len(sha) < 7 {
		return ""
	}
	for i := 0; i < 7; i++ {
		c := sha[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ""
		}
	}
	return sha[:7]
}

// worktreeMarker picks the SHA-or-sentinel suffix used in TriggerKey:
//
//   - if the working tree is dirty → "local-dirty"
//   - else if HeadRevision parses as a short SHA → that 7-char prefix
//   - else → "no-git"
func worktreeMarker(src TriggerSource) string {
	if strings.EqualFold(src.WorkingTree, WorkingTreeDirty) {
		return "local-dirty"
	}
	if s := shortSHA(src.HeadRevision); s != "" {
		return s
	}
	return "no-git"
}
