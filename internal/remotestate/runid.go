package remotestate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"
)

// DeriveRunID computes the run ID according to the documented precedence:
//
//  1. explicitID (from --exec-id or ORUN_EXEC_ID)
//  2. GitHub Actions: gha_{GITHUB_RUN_ID}_{GITHUB_RUN_ATTEMPT}
//  3. Local fallback: local_{ts_hex}_{rand_hex}
func DeriveRunID(explicitID string) string {
	if explicitID != "" {
		return explicitID
	}

	if os.Getenv("GITHUB_ACTIONS") == "true" {
		ghaRunID := os.Getenv("GITHUB_RUN_ID")
		attempt := os.Getenv("GITHUB_RUN_ATTEMPT")
		if attempt == "" {
			attempt = "1"
		}
		if ghaRunID != "" {
			return fmt.Sprintf("gha_%s_%s", ghaRunID, attempt)
		}
	}

	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("local_%s_%s", ts, hex.EncodeToString(b[:]))
}

// crockfordAlphabet is the Crockford base32 alphabet (no I, L, O, U), uppercase,
// matching the platform's run-ULID validator (`/^[0-9A-HJKMNP-TV-Z]{26}$/`).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// RunULID maps the CLI's execution id (any string: an explicit --exec-id, the
// GitHub `gha_<runId>_<attempt>` form, or the local fallback) to a contract-valid
// run ULID. The platform requires `runId` to be a 26-char Crockford base32 ULID
// (state-api-contract §0) and rejects the CLI's raw execId; this derives a stable
// ULID-shaped id from it so that the run identity — and therefore idempotent
// create/replay and crash resume — is preserved across invocations: the same
// execId always yields the same wire run id.
//
// The derivation is a SHA-256 of the execId folded to 128 bits and rendered as
// 26 Crockford base32 characters. It is deterministic, not time-sortable; the
// platform orders runs by createdAt, so sortability of the id is not relied upon.
func RunULID(execID string) string {
	sum := sha256.Sum256([]byte(execID))
	// Fold the 256-bit digest into 128 bits so the value fits a ULID's 16 bytes.
	var b [16]byte
	for i := 0; i < 16; i++ {
		b[i] = sum[i] ^ sum[i+16]
	}
	return encodeCrockford128(b[:])
}

// encodeCrockford128 renders a 16-byte (128-bit) value as exactly 26 Crockford
// base32 characters (the ULID text length). The most-significant character only
// carries the top 3 bits, so it is always '0'..'7' — well within the alphabet.
func encodeCrockford128(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(32)
	rem := new(big.Int)
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		n.DivMod(n, base, rem)
		out[i] = crockfordAlphabet[rem.Int64()]
	}
	return string(out)
}
