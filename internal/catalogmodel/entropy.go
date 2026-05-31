package catalogmodel

import (
	"crypto/rand"
	"math"
	mathrand "math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// monoMu + monoEntropy are the package-level monotonic ULID source used by
// NewSourceSnapshotID / NewCatalogSnapshotID / NewComponentID. The triggerctx
// package uses the same pattern for its trg_ ULIDs (see
// internal/triggerctx/ids.go) — we keep an independent source here so
// catalogmodel stays import-isolated from any sibling internal/* package
// (constraint #1 in task-0023.md).
var (
	monoMu      sync.Mutex
	monoEntropy *ulid.MonotonicEntropy
)

// nowFn is the clock used by newULID. Tests may override it via
// internal/test_hooks.go (kept off the public surface).
var nowFn = time.Now

func init() {
	seed := readCryptoSeed()
	monoEntropy = ulid.Monotonic(mathrand.New(mathrand.NewSource(seed)), 0)
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
