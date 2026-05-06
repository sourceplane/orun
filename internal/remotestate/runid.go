package remotestate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
