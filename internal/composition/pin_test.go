package composition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestPinIntentDigestsInsertsAndPreservesFormatting(t *testing.T) {
	input := `# workspace intent
apiVersion: sourceplane.io/v1
kind: Intent

compositions:
  # platform compositions
  sources:
    - name: platform   # local dir
      kind: dir
      path: ./compositions
      metadata:
        owner: team-a
    - name: remote
      kind: oci
      ref: ghcr.io/acme/compositions:v1 # tag
      digest: "sha256:old" # pinned
    - name: untouched
      kind: dir
      path: ./other

components: []
`
	sources := []model.ResolvedCompositionSource{
		{Name: "platform", Kind: "dir", ResolvedDigest: "sha256:aaa"},
		{Name: "remote", Kind: "oci", ResolvedDigest: "sha256:bbb"},
		{Name: legacySourceName, Kind: legacySourceKind, ResolvedDigest: "sha256:zzz"},
	}

	out, changes, err := pinIntentDigests([]byte(input), sources)
	if err != nil {
		t.Fatalf("pinIntentDigests: %v", err)
	}

	want := strings.Replace(input,
		"      path: ./compositions\n",
		"      path: ./compositions\n      digest: sha256:aaa\n", 1)
	want = strings.Replace(want, `digest: "sha256:old" # pinned`, `digest: "sha256:bbb" # pinned`, 1)
	if string(out) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", out, want)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %+v", changes)
	}
	if changes[0].Name != "platform" || changes[0].OldDigest != "" || !changes[0].Changed() {
		t.Fatalf("unexpected platform change: %+v", changes[0])
	}
	if changes[1].Name != "remote" || changes[1].OldDigest != "sha256:old" || changes[1].NewDigest != "sha256:bbb" {
		t.Fatalf("unexpected remote change: %+v", changes[1])
	}

	// Re-running with the same digests is a no-op.
	again, changes, err := pinIntentDigests(out, sources)
	if err != nil {
		t.Fatalf("second pin: %v", err)
	}
	if string(again) != string(out) {
		t.Fatalf("second pin rewrote the intent:\n%s", again)
	}
	for _, change := range changes {
		if change.Changed() {
			t.Fatalf("expected no changes on re-pin, got %+v", change)
		}
	}
}

func TestPinIntentDigestsReplacesPlainAndSingleQuoted(t *testing.T) {
	input := "compositions:\n  sources:\n  - name: a\n    kind: dir\n    path: ./a\n    digest: sha256:old\n  - name: b\n    kind: dir\n    digest: 'sha256:old'\n    path: ./b\n"
	out, _, err := pinIntentDigests([]byte(input), []model.ResolvedCompositionSource{
		{Name: "a", Kind: "dir", ResolvedDigest: "sha256:new"},
		{Name: "b", Kind: "dir", ResolvedDigest: "sha256:new2"},
	})
	if err != nil {
		t.Fatalf("pinIntentDigests: %v", err)
	}
	want := "compositions:\n  sources:\n  - name: a\n    kind: dir\n    path: ./a\n    digest: sha256:new\n  - name: b\n    kind: dir\n    digest: 'sha256:new2'\n    path: ./b\n"
	if string(out) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", out, want)
	}
}

func TestPinIntentDigestsErrors(t *testing.T) {
	cases := map[string]struct {
		input   string
		sources []model.ResolvedCompositionSource
		want    string
	}{
		"no sources": {
			input: "kind: Intent\n",
			want:  "does not declare compositions.sources",
		},
		"undeclared source": {
			input:   "compositions:\n  sources:\n    - name: a\n      kind: dir\n      path: ./a\n",
			sources: []model.ResolvedCompositionSource{{Name: "b", Kind: "dir", ResolvedDigest: "sha256:x"}},
			want:    "not declared",
		},
		"flow style": {
			input:   "compositions:\n  sources:\n    - {name: a, kind: dir, path: ./a}\n",
			sources: []model.ResolvedCompositionSource{{Name: "a", Kind: "dir", ResolvedDigest: "sha256:x"}},
			want:    "flow style",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := pinIntentDigests([]byte(tc.input), tc.sources)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPinIntentDigestsEnforcedByResolution(t *testing.T) {
	workspace := t.TempDir()
	writeCompositionPackage(t, filepath.Join(workspace, "compositions"), "platform", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo deploy"},
	})

	intentPath := filepath.Join(workspace, "intent.yaml")
	intentYAML := "apiVersion: sourceplane.io/v1\nkind: Intent\nmetadata:\n  name: demo\ncompositions:\n  sources:\n    # comment kept\n    - name: platform\n      kind: dir\n      path: ./compositions\n"
	if err := os.WriteFile(intentPath, []byte(intentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	intent := &model.Intent{Compositions: model.CompositionConfig{Sources: []model.CompositionSource{{Name: "platform", Kind: "dir", Path: "./compositions"}}}}
	registry, err := LoadRegistry(intent, intentPath, "")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, err := PinIntentDigests(intentPath, registry.Sources); err != nil {
		t.Fatalf("PinIntentDigests: %v", err)
	}

	data, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# comment kept") || !strings.Contains(string(data), "digest: "+registry.Sources[0].ResolvedDigest) {
		t.Fatalf("intent not pinned as expected:\n%s", data)
	}

	// The pinned digest is enforced: a change to the source now fails resolution.
	pinned := &model.Intent{Compositions: model.CompositionConfig{Sources: []model.CompositionSource{{Name: "platform", Kind: "dir", Path: "./compositions", Digest: registry.Sources[0].ResolvedDigest}}}}
	if _, err := LoadRegistry(pinned, intentPath, ""); err != nil {
		t.Fatalf("pinned resolution failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "compositions", "extra.txt"), []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(pinned, intentPath, ""); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestReadLockFileAndCheckLockDrift(t *testing.T) {
	intentPath := filepath.Join(t.TempDir(), "intent.yaml")

	lock, err := ReadLockFile(intentPath)
	if err != nil || lock != nil {
		t.Fatalf("expected no lock, got %+v, %v", lock, err)
	}

	recorded := []model.ResolvedCompositionSource{
		{Name: "a", Kind: "dir", ResolvedDigest: "sha256:a1"},
		{Name: "b", Kind: "dir", ResolvedDigest: "sha256:b1"},
		{Name: "c", Kind: "dir", ResolvedDigest: "sha256:c1"},
	}
	if err := WriteLockFile(intentPath, recorded); err != nil {
		t.Fatal(err)
	}
	lock, err = ReadLockFile(intentPath)
	if err != nil || lock == nil || len(lock.Sources) != 3 {
		t.Fatalf("ReadLockFile: %+v, %v", lock, err)
	}

	declared := []model.CompositionSource{{Name: "a"}, {Name: "b", Digest: "sha256:b2"}, {Name: "c"}, {Name: "d"}}
	current := []model.ResolvedCompositionSource{
		{Name: "a", Kind: "dir", ResolvedDigest: "sha256:a2"}, // drifted, unpinned
		{Name: "b", Kind: "dir", ResolvedDigest: "sha256:b2"}, // pinned: skipped
		{Name: "c", Kind: "dir", ResolvedDigest: "sha256:c1"}, // unchanged
		{Name: "d", Kind: "dir", ResolvedDigest: "sha256:d1"}, // not in lock
	}
	drift, unlocked := CheckLockDrift(declared, current, lock)
	if len(drift) != 1 || drift[0] != (LockDrift{Name: "a", LockedDigest: "sha256:a1", Digest: "sha256:a2"}) {
		t.Fatalf("unexpected drift: %+v", drift)
	}
	if len(unlocked) != 1 || unlocked[0] != "d" {
		t.Fatalf("unexpected unlocked: %+v", unlocked)
	}
}
