package work

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID prefixes per data-model.md §1. Used both to construct fresh identifiers
// and to validate untrusted input.
const (
	IDPrefixInitiative = "ini_"
	IDPrefixEpic       = "epc_"
	IDPrefixTask       = "tsk_"
	IDPrefixPrincipal  = "prn_"
	IDPrefixEvent      = "wev_"
)

// monoMu + monoEntropy are the package-level monotonic ULID source. We keep an
// independent source (rather than importing a sibling package) so work stays
// import-isolated, mirroring internal/catalogmodel and internal/triggerctx.
var (
	monoMu      sync.Mutex
	monoEntropy *ulid.MonotonicEntropy
)

// nowFn is the clock used when minting ULIDs. Tests may override it for
// deterministic timestamps; event timestamps themselves are always passed in
// explicitly so the mutators stay pure.
var nowFn = time.Now

func init() {
	monoEntropy = ulid.Monotonic(mathrand.New(mathrand.NewSource(readCryptoSeed())), 0)
}

func readCryptoSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	var v int64
	for i := 0; i < 8; i++ {
		v = (v << 8) | int64(b[i])
	}
	if v == math.MinInt64 {
		v = 0
	}
	return v
}

func nextULID() string {
	monoMu.Lock()
	defer monoMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(nowFn()), monoEntropy).String()
}

// NewInitiativeID returns a fresh ini_<ulid> identifier.
func NewInitiativeID() string { return IDPrefixInitiative + nextULID() }

// NewEpicID returns a fresh epc_<ulid> identifier.
func NewEpicID() string { return IDPrefixEpic + nextULID() }

// NewTaskID returns a fresh tsk_<ulid> identifier.
func NewTaskID() string { return IDPrefixTask + nextULID() }

// NewPrincipalID returns a fresh prn_<ulid> identifier.
func NewPrincipalID() string { return IDPrefixPrincipal + nextULID() }

// NewEventID returns a fresh wev_<ulid> identifier.
func NewEventID() string { return IDPrefixEvent + nextULID() }

// NewItemID returns a fresh identifier for the given entity kind.
func NewItemID(k Kind) string {
	switch k {
	case KindInitiative:
		return NewInitiativeID()
	case KindEpic:
		return NewEpicID()
	default:
		return NewTaskID()
	}
}

var (
	prefixPattern    = regexp.MustCompile(`^[A-Z]{2,5}$`)
	taskKeyPattern   = regexp.MustCompile(`^[A-Z]{2,5}-[1-9][0-9]*$`)
	slugPattern      = regexp.MustCompile(`^[a-z0-9-]+$`)
	componentPattern = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`)
)

// ErrInvalidKey is returned by the key validators on malformed input. Callers
// may wrap it with their own context; errors.Is(err, ErrInvalidKey) holds.
var ErrInvalidKey = errors.New("work: invalid key")

// ValidatePrefix checks a project task-key prefix: 2–5 uppercase ASCII letters
// (data-model.md §1).
func ValidatePrefix(prefix string) error {
	if !prefixPattern.MatchString(prefix) {
		return fmt.Errorf("%w: prefix %q must be 2-5 uppercase letters", ErrInvalidKey, prefix)
	}
	return nil
}

// ValidateTaskKey checks a task human key of the form PREFIX-seq (e.g. ORN-142).
func ValidateTaskKey(key string) error {
	if !taskKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: task key %q must be PREFIX-<seq>", ErrInvalidKey, key)
	}
	return nil
}

// ValidateSlug checks an Epic/Initiative slug: lowercase alphanumerics and
// hyphens (data-model.md §1).
func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: slug %q must match [a-z0-9-]+", ErrInvalidKey, slug)
	}
	return nil
}

// ValidateComponentKey checks a catalog component key referenced from a
// contract's affects[]: the three-segment <namespace>/<repo>/<name> form.
func ValidateComponentKey(key string) error {
	if !componentPattern.MatchString(key) {
		return fmt.Errorf("%w: component key %q must be <namespace>/<repo>/<name>", ErrInvalidKey, key)
	}
	return nil
}

// FormatTaskKey assembles a task human key from a validated prefix and a 1-based
// sequence number.
func FormatTaskKey(prefix string, seq int64) string {
	return fmt.Sprintf("%s-%d", prefix, seq)
}

// FormatWorkKey assembles the fully-qualified work key
// <org>/<project>/<human-key>. project is the existing remote "<org>/<project>"
// routing pair; humanKey is the task key or Epic/Initiative slug path.
func FormatWorkKey(project, humanKey string) string {
	return project + "/" + humanKey
}

// ParseWorkKey splits a fully-qualified work key into its <org>/<project> pair
// and the trailing human key. It does not validate the human key shape (it may
// be a task key or a slug path).
func ParseWorkKey(s string) (project, humanKey string, err error) {
	parts := strings.Split(s, "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("%w: work key %q must be <org>/<project>/<human-key>", ErrInvalidKey, s)
	}
	project = parts[0] + "/" + parts[1]
	humanKey = strings.Join(parts[2:], "/")
	return project, humanKey, nil
}
