package statebackend_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (go.mod) from working directory")
		}
		dir = parent
	}
}

// recordedChecksum reads the expected "<sha256>  <filename>" line from a
// CHECKSUM file, skipping comment/blank lines.
func recordedChecksum(t *testing.T, checksumPath, filename string) string {
	t.Helper()
	f, err := os.Open(checksumPath)
	if err != nil {
		t.Fatalf("open CHECKSUM: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == filename {
			return fields[0]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan CHECKSUM: %v", err)
	}
	t.Fatalf("CHECKSUM has no entry for %s", filename)
	return ""
}

// TestVendoredCoordinationChecksum is the drift guard for the vendored
// coordination wire contract. It fails if
// specs/orun-native-coordination/vendored/coordination-api.md no longer matches
// the sha256 recorded in the sibling CHECKSUM file. When this fails, re-vendor
// from orun-cloud or renegotiate the contract (see that directory's README.md).
func TestVendoredCoordinationChecksum(t *testing.T) {
	root := repoRoot(t)
	vendoredDir := filepath.Join(root, "specs", "orun-native-coordination", "vendored")
	contractPath := filepath.Join(vendoredDir, "coordination-api.md")
	checksumPath := filepath.Join(vendoredDir, "CHECKSUM")

	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read vendored contract: %v", err)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	want := recordedChecksum(t, checksumPath, "coordination-api.md")

	if got != want {
		t.Fatalf(
			"vendored coordination-api.md drifted from its recorded checksum:\n"+
				"  recorded (CHECKSUM): %s\n"+
				"  actual   (file):     %s\n"+
				"re-vendor from orun-cloud or renegotiate the contract, then update\n"+
				"specs/orun-native-coordination/vendored/CHECKSUM (see that dir's README.md).",
			want, got,
		)
	}
}
