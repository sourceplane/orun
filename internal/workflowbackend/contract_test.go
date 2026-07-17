package workflowbackend

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// contractDir is the vendored contract/v1 directory. The same files are
// vendored byte-identically in sourceplane/torkflow; both CIs run this
// conformance suite against them (invariant 9).
const contractDir = "contract/v1"

// TestContractManifest verifies every vendored contract file matches
// MANIFEST.sha256 — a local edit that diverges from the vendored bytes fails
// here. Changing the contract means changing the manifest in BOTH repos, in the
// same change; a new wire shape is a new contract directory.
func TestContractManifest(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join(contractDir, "MANIFEST.sha256"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	want := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("malformed manifest line: %q", line)
		}
		want[parts[1]] = parts[0]
	}

	got := map[string]string{}
	err = filepath.Walk(contractDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || filepath.Base(path) == "MANIFEST.sha256" {
			return werr
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(contractDir, path)
		got[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if want[n] == "" {
			t.Errorf("contract file %s is not in MANIFEST.sha256 — add it (and mirror to torkflow)", n)
		} else if want[n] != got[n] {
			t.Errorf("contract file %s diverges from the vendored manifest — the wire contract may not drift (got %s want %s)", n, got[n], want[n])
		}
	}
	for n := range want {
		if got[n] == "" {
			t.Errorf("manifest names %s but the file is missing", n)
		}
	}
}

// TestRequestFixturesRoundTrip proves the Go Request type and the vendored
// request fixtures agree: every fixture unmarshals losslessly (no unknown
// fields) and re-marshals to semantically identical JSON.
func TestRequestFixturesRoundTrip(t *testing.T) {
	for _, name := range []string{"request-minimal.json", "request-full.json"} {
		t.Run(name, func(t *testing.T) {
			data := readFixture(t, name)
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			var req Request
			if err := dec.Decode(&req); err != nil {
				t.Fatalf("Request cannot decode fixture: %v", err)
			}
			if req.Contract != ContractV1 || req.Workflow == "" {
				t.Fatalf("fixture missing required fields: %+v", req)
			}
			assertJSONEqual(t, data, req)
		})
	}
}

// TestResponseFixturesRoundTrip proves the Go Result type and the vendored
// response fixtures agree.
func TestResponseFixturesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name       string
		wantStatus string
	}{
		{"response-success.json", StatusSuccess},
		{"response-failed.json", StatusFailed},
		{"response-paused.json", StatusPaused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := readFixture(t, tc.name)
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			var res Result
			if err := dec.Decode(&res); err != nil {
				t.Fatalf("Result cannot decode fixture: %v", err)
			}
			if res.Contract != ContractV1 || res.Status != tc.wantStatus {
				t.Fatalf("fixture decoded wrong: %+v", res)
			}
			assertJSONEqual(t, data, res)
		})
	}
	if readFixture(t, "response-success.json") != nil {
		var res Result
		_ = json.Unmarshal(readFixture(t, "response-success.json"), &res)
		if res.Outputs["email"] != "oncall@example.dev" {
			t.Fatalf("declared outputs did not survive the round trip: %#v", res.Outputs)
		}
	}
}

// TestUnknownContractRejected: a future-contract response is a versioned error,
// not a silent misparse.
func TestUnknownContractRejected(t *testing.T) {
	bin := writeFakeEngine(t, `echo '{"contract":"v9","status":"success"}'`)
	eng, err := ResolveEngine(EngineOptions{Bin: bin, Args: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Invoke(nil, Request{Workflow: "wf.yaml"})
	if err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("expected a versioned contract error, got %v", err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractDir, "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// assertJSONEqual re-marshals v and compares it to the fixture semantically
// (key order and whitespace insensitive).
func assertJSONEqual(t *testing.T, fixture []byte, v any) {
	t.Helper()
	remarshaled, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(fixture, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(remarshaled, &b); err != nil {
		t.Fatal(err)
	}
	na, _ := json.Marshal(a)
	nb, _ := json.Marshal(b)
	// Compare canonical forms via unmarshal-to-any → marshal (map keys sorted).
	if string(na) != string(nb) {
		t.Fatalf("round trip diverges:\nfixture:    %s\nremarshal:  %s", na, nb)
	}
}
