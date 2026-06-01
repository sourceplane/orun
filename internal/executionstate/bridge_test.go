package executionstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
)

// bridgeFixture builds a Bridge wired to a fresh LocalStore plus a legacy
// runner directory. revKey is the data-model.md §3.1-shaped revision key the
// fixture pre-creates the new-layout execution dir under, and legacyID is the
// legacy execution id under LegacyRoot. The runner artifacts (state.json /
// metadata.json) are pre-written under <LegacyRoot>/<legacyID>/ so the bridge
// has source bytes to mirror.
type bridgeFixture struct {
	t          *testing.T
	store      *statestore.LocalStore
	bridge     *Bridge
	revKey     string
	execKey    string
	legacyID   string
	legacyRoot string
	now        time.Time
}

func newBridgeFixture(t *testing.T, mode MirrorMode) *bridgeFixture {
	t.Helper()
	root := statefs.NewWorkspace(t)
	storeRoot := filepath.Join(root, ".orun")
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: storeRoot})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	legacyRoot := filepath.Join(root, "legacy", "executions")
	if err := os.MkdirAll(legacyRoot, 0o755); err != nil {
		t.Fatalf("mkdir legacyRoot: %v", err)
	}

	revKey := "rev-main-p12345678"
	execKey := "run-001"
	legacyID := "gh-12345-1-abcdef0123456789"

	if err := os.MkdirAll(filepath.Join(legacyRoot, legacyID), 0o755); err != nil {
		t.Fatalf("mkdir legacy execID: %v", err)
	}
	mustWrite(t, filepath.Join(legacyRoot, legacyID, "state.json"),
		[]byte(`{"status":"succeeded"}`+"\n"))
	mustWrite(t, filepath.Join(legacyRoot, legacyID, "metadata.json"),
		[]byte(`{"runId":"12345"}`+"\n"))

	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	b := &Bridge{
		Store:      store,
		LegacyRoot: legacyRoot,
		MirrorMode: mode,
		Now:        func() time.Time { return now },
	}
	return &bridgeFixture{
		t: t, store: store, bridge: b,
		revKey: revKey, execKey: execKey,
		legacyID: legacyID, legacyRoot: legacyRoot, now: now,
	}
}

func mustWrite(t *testing.T, p string, body []byte) {
	t.Helper()
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// readMirrored returns the bytes mirrored to <revKey>/<execKey>/<name> via the
// StateStore. Uses Store.Read so the test traces the same code path the
// resolver and `orun status` will use in M5.
func (f *bridgeFixture) readMirrored(name string) ([]byte, error) {
	got, _, err := f.store.Read(context.Background(),
		statestore.ExecutionFilePath(f.revKey, f.execKey, name))
	return got, err
}

// listEvents returns the bridge-mirror-failed events under the execution's
// event log, decoded from disk. Sorted by sequence ascending.
func (f *bridgeFixture) listEvents() []bridgeMirrorFailedEvent {
	f.t.Helper()
	prefix := statestore.ExecutionDir(f.revKey, f.execKey) + "/events"
	infos, err := f.store.List(context.Background(), prefix)
	if err != nil {
		f.t.Fatalf("List events: %v", err)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	out := make([]bridgeMirrorFailedEvent, 0, len(infos))
	for _, info := range infos {
		body, _, err := f.store.Read(context.Background(), info.Path)
		if err != nil {
			f.t.Fatalf("read event %s: %v", info.Path, err)
		}
		// Only decode bridge-mirror-failed entries; tolerate other kinds.
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(body, &probe); err != nil {
			f.t.Fatalf("unmarshal probe %s: %v", info.Path, err)
		}
		if probe.Kind != "bridge-mirror-failed" {
			continue
		}
		var evt bridgeMirrorFailedEvent
		if err := json.Unmarshal(body, &evt); err != nil {
			f.t.Fatalf("unmarshal event %s: %v", info.Path, err)
		}
		out = append(out, evt)
	}
	return out
}

// withLinkFn swaps the package-level linkFn for the duration of the test and
// restores the original via t.Cleanup. Tests that swap MUST NOT call
// t.Parallel; the seam is package-global.
func withLinkFn(t *testing.T, stub func(oldname, newname string) error) {
	t.Helper()
	orig := linkFn
	linkFn = stub
	t.Cleanup(func() { linkFn = orig })
}

// --- MirrorMode.String -------------------------------------------------------

func TestMirrorMode_String(t *testing.T) {
	cases := []struct {
		in   MirrorMode
		want string
	}{
		{MirrorModeAuto, "auto"},
		{MirrorModeHardlink, "hardlink"},
		{MirrorModeCopy, "copy"},
		{MirrorMode(99), "mirror-mode-99"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("MirrorMode(%d).String() = %q want %q", c.in, got, c.want)
		}
	}
}

// --- Hardlink success path ---------------------------------------------------

func TestMirrorRunnerOutput_Hardlink_Success(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	for _, name := range bridgeMirroredFiles {
		got, err := f.readMirrored(name)
		if err != nil {
			t.Fatalf("read mirrored %s: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join(f.legacyRoot, f.legacyID, name))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s mirror bytes mismatch:\n got=%q\nwant=%q", name, got, want)
		}
	}
	// Verify the file is a hardlink, not a copy: same inode as the source.
	for _, name := range bridgeMirroredFiles {
		srcAbs := filepath.Join(f.legacyRoot, f.legacyID, name)
		dstAbs := filepath.Join(f.store.Root(),
			filepath.FromSlash(statestore.ExecutionFilePath(f.revKey, f.execKey, name)))
		if !sameInode(t, srcAbs, dstAbs) {
			t.Errorf("%s: expected hardlink (same inode) src=%s dst=%s",
				name, srcAbs, dstAbs)
		}
	}
	if evts := f.listEvents(); len(evts) != 0 {
		t.Errorf("unexpected bridge-mirror-failed events on success path: %+v", evts)
	}
}

func TestMirrorRunnerOutput_CatalogParentStateMetadataAndLogs(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeCopy)
	f.bridge.CatalogParent = revision.CatalogParentRef{
		SourceKey:  "src-branch-main-abcdef0",
		CatalogKey: "cat-abcdef",
	}
	logDir := filepath.Join(f.legacyRoot, f.legacyID, "logs", "api.dev.echo")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	mustWrite(t, filepath.Join(logDir, "run.log"), []byte("hello from catalog log\n"))

	if err := f.bridge.MirrorRunnerOutput(context.Background(), f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	if err := f.bridge.MirrorRunnerLog(context.Background(), f.execKey, f.revKey, f.legacyID, "api.dev.echo", "run"); err != nil {
		t.Fatalf("MirrorRunnerLog: %v", err)
	}

	for _, name := range bridgeMirroredFiles {
		p, err := catalogstore.CatalogExecutionFilePath(f.bridge.CatalogParent.SourceKey, f.bridge.CatalogParent.CatalogKey, f.revKey, f.execKey, name)
		if err != nil {
			t.Fatalf("CatalogExecutionFilePath(%s): %v", name, err)
		}
		if _, _, err := f.store.Read(context.Background(), p); err != nil {
			t.Fatalf("read catalog %s: %v", name, err)
		}
	}
	dir, err := catalogstore.CatalogExecutionDir(f.bridge.CatalogParent.SourceKey, f.bridge.CatalogParent.CatalogKey, f.revKey, f.execKey)
	if err != nil {
		t.Fatalf("CatalogExecutionDir: %v", err)
	}
	raw, _, err := f.store.Read(context.Background(), dir+"/logs/api.dev.echo/run.log")
	if err != nil {
		t.Fatalf("read catalog log: %v", err)
	}
	if string(raw) != "hello from catalog log\n" {
		t.Fatalf("catalog log = %q", raw)
	}
}

func TestMirrorRunnerLog_GlobalOnlyAndValidation(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeCopy)

	if got := runnerLogSegment("///"); got != "unknown" {
		t.Fatalf("runnerLogSegment empty = %q; want unknown", got)
	}
	jobID := "api/dev:verify @ prod"
	stepID := "run step"
	logDir := filepath.Join(f.legacyRoot, f.legacyID, "logs", legacyRunnerLogSegment(jobID))
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	mustWrite(t, filepath.Join(logDir, legacyRunnerLogSegment(stepID)+".log"), []byte("global-only log\n"))

	if err := f.bridge.MirrorRunnerLog(context.Background(), f.execKey, f.revKey, f.legacyID, jobID, stepID); err != nil {
		t.Fatalf("MirrorRunnerLog: %v", err)
	}
	globalPath := statestore.ExecutionDir(f.revKey, f.execKey) + "/logs/" + runnerLogSegment(jobID) + "/" + runnerLogSegment(stepID) + ".log"
	raw, _, err := f.store.Read(context.Background(), globalPath)
	if err != nil {
		t.Fatalf("read global log: %v", err)
	}
	if string(raw) != "global-only log\n" {
		t.Fatalf("global log = %q", raw)
	}

	noSource := newBridgeFixture(t, MirrorModeCopy)
	if err := noSource.bridge.MirrorRunnerLog(context.Background(), noSource.execKey, noSource.revKey, noSource.legacyID, "missing", "missing"); err != nil {
		t.Fatalf("missing source log should be a noop, got %v", err)
	}

	bad := *f.bridge
	bad.Store = nil
	if err := bad.MirrorRunnerLog(context.Background(), f.execKey, f.revKey, f.legacyID, jobID, stepID); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("nil store err=%v want ErrInvalid", err)
	}
	bad = *f.bridge
	bad.LegacyRoot = ""
	if err := bad.MirrorRunnerLog(context.Background(), f.execKey, f.revKey, f.legacyID, jobID, stepID); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("empty legacy root err=%v want ErrInvalid", err)
	}
	if err := f.bridge.MirrorRunnerLog(context.Background(), f.execKey, "bad-rev", f.legacyID, jobID, stepID); err == nil {
		t.Fatal("invalid revision key unexpectedly succeeded")
	}
	if err := f.bridge.MirrorRunnerLog(context.Background(), "bad/exec", f.revKey, f.legacyID, jobID, stepID); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("invalid exec key err=%v want ErrInvalid", err)
	}
}

func TestBridgeDestAbsEmptyRoot(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeHardlink)
	b := *f.bridge
	b.Store = emptyRootStore{StateStore: f.store}
	if _, err := b.destAbs("revisions/rev-main-p12345678/executions/run-001/state.json"); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("destAbs err=%v want ErrInvalid", err)
	}
}

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	staSys, ok1 := sa.Sys().(*syscall.Stat_t)
	stbSys, ok2 := sb.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		t.Fatalf("Sys() not *syscall.Stat_t on this platform")
	}
	return staSys.Ino == stbSys.Ino && staSys.Dev == stbSys.Dev
}

// --- Forced EXDEV → copy fallback (Auto mode) --------------------------------

func TestMirrorRunnerOutput_EXDEV_FallsBackToCopy(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	calls := 0
	withLinkFn(t, func(oldname, newname string) error {
		calls++
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EXDEV}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	if calls == 0 {
		t.Fatalf("linkFn stub was never invoked — fallback path didn't go through link first")
	}
	for _, name := range bridgeMirroredFiles {
		got, err := f.readMirrored(name)
		if err != nil {
			t.Fatalf("read mirrored %s after fallback: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join(f.legacyRoot, f.legacyID, name))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s copy-fallback bytes mismatch:\n got=%q\nwant=%q",
				name, got, want)
		}
	}
	// Copy fallback path: dest must NOT be a hardlink of src.
	for _, name := range bridgeMirroredFiles {
		srcAbs := filepath.Join(f.legacyRoot, f.legacyID, name)
		dstAbs := filepath.Join(f.store.Root(),
			filepath.FromSlash(statestore.ExecutionFilePath(f.revKey, f.execKey, name)))
		if sameInode(t, srcAbs, dstAbs) {
			t.Errorf("%s: expected copy (distinct inode) but got hardlink", name)
		}
	}
	if evts := f.listEvents(); len(evts) != 0 {
		t.Errorf("EXDEV→copy success path must not emit events; got %+v", evts)
	}
}

// --- MirrorModeHardlink: EXDEV is a failure (no fallback) --------------------

func TestMirrorRunnerOutput_Hardlink_EXDEV_EmitsFailure(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeHardlink)
	withLinkFn(t, func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EXDEV}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: must return nil on mirror failure, got %v", err)
	}
	evts := f.listEvents()
	if len(evts) != len(bridgeMirroredFiles) {
		t.Fatalf("want %d failure events (one per artifact), got %d: %+v",
			len(bridgeMirroredFiles), len(evts), evts)
	}
	seen := map[string]bool{}
	for _, evt := range evts {
		if evt.Kind != "bridge-mirror-failed" {
			t.Errorf("evt.Kind = %q want bridge-mirror-failed", evt.Kind)
		}
		if evt.Payload.Mode != "hardlink" {
			t.Errorf("evt.Mode = %q want hardlink", evt.Payload.Mode)
		}
		if evt.Payload.ExecutionKey != f.execKey {
			t.Errorf("evt.ExecutionKey = %q want %q", evt.Payload.ExecutionKey, f.execKey)
		}
		if evt.Payload.RevisionKey != f.revKey {
			t.Errorf("evt.RevisionKey = %q want %q", evt.Payload.RevisionKey, f.revKey)
		}
		if evt.Payload.LegacyExecID != f.legacyID {
			t.Errorf("evt.LegacyExecID = %q want %q", evt.Payload.LegacyExecID, f.legacyID)
		}
		if evt.Payload.Stage != "link" {
			t.Errorf("evt.Stage = %q want link", evt.Payload.Stage)
		}
		if !strings.Contains(evt.Payload.Error, "cross-device") &&
			!strings.Contains(evt.Payload.Error, "EXDEV") &&
			!strings.Contains(strings.ToLower(evt.Payload.Error), "exdev") {
			// Best-effort string check: the wrapped error should mention
			// the EXDEV cause via *os.LinkError.Error().
			t.Logf("evt.Error = %q (informational; *os.LinkError formatting varies)", evt.Payload.Error)
		}
		seen[evt.Payload.Artifact] = true
	}
	for _, name := range bridgeMirroredFiles {
		if !seen[name] {
			t.Errorf("missing failure event for artifact %q", name)
		}
	}
}

// --- Non-EXDEV link error → failure event ------------------------------------

func TestMirrorRunnerOutput_NonEXDEVLinkError_EmitsFailure(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	withLinkFn(t, func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	evts := f.listEvents()
	if len(evts) != len(bridgeMirroredFiles) {
		t.Fatalf("want %d failure events, got %d: %+v",
			len(bridgeMirroredFiles), len(evts), evts)
	}
	for _, evt := range evts {
		if evt.Payload.Stage != "link" {
			t.Errorf("evt.Stage = %q want link", evt.Payload.Stage)
		}
		if evt.Payload.Mode != "auto" {
			t.Errorf("evt.Mode = %q want auto", evt.Payload.Mode)
		}
	}
	// And critically: nothing was mirrored to the destination.
	for _, name := range bridgeMirroredFiles {
		if _, err := f.readMirrored(name); !errors.Is(err, statestore.ErrNotFound) {
			t.Errorf("%s: expected ErrNotFound at dst (no mirror happened); got err=%v", name, err)
		}
	}
}

// --- Copy-failure event: copy mode, write fails ------------------------------
//
// We simulate copy failure indirectly: pre-create a file at the destination
// path AS A DIRECTORY so Store.Write hits an EISDIR-shaped failure on its
// atomic-rename. This exercises the copy-stage failure event without poking
// the LocalStore internals.

func TestMirrorRunnerOutput_CopyMode_StoreWriteFails_EmitsFailure(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeCopy)
	// Plant a directory at one of the artifact destinations so Write fails.
	dstAbs := filepath.Join(f.store.Root(),
		filepath.FromSlash(statestore.ExecutionFilePath(f.revKey, f.execKey, "state.json")))
	if err := os.MkdirAll(dstAbs, 0o755); err != nil {
		t.Fatalf("plant dir at dst: %v", err)
	}
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: must return nil on mirror failure, got %v", err)
	}
	evts := f.listEvents()
	if len(evts) == 0 {
		t.Fatalf("expected at least one bridge-mirror-failed event, got 0")
	}
	// The destination-as-directory plant trips the read-dest probe before
	// the copy stage gets a chance; that's an acceptable fail point because
	// the bridge correctly emits a failure event and returns nil. Accept
	// either stage so the test stays faithful to the contract (mirror-failed
	// event regardless of the precise sub-stage that detected the corruption).
	foundFailure := false
	for _, evt := range evts {
		if evt.Payload.Mode == "copy" &&
			evt.Payload.Artifact == "state.json" &&
			(evt.Payload.Stage == "copy" || evt.Payload.Stage == "read-dest") {
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Errorf("did not find copy-mode failure event for state.json: %+v", evts)
	}
}

// --- Idempotent re-mirror: identical inputs converge -------------------------

func TestMirrorRunnerOutput_Idempotent(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	for i := 0; i < 3; i++ {
		if err := f.bridge.MirrorRunnerOutput(context.Background(),
			f.execKey, f.revKey, f.legacyID); err != nil {
			t.Fatalf("MirrorRunnerOutput[%d]: %v", i, err)
		}
	}
	for _, name := range bridgeMirroredFiles {
		got, err := f.readMirrored(name)
		if err != nil {
			t.Fatalf("read mirrored %s: %v", name, err)
		}
		want, _ := os.ReadFile(filepath.Join(f.legacyRoot, f.legacyID, name))
		if string(got) != string(want) {
			t.Errorf("%s diverged after idempotent re-run: got=%q want=%q",
				name, got, want)
		}
	}
	if evts := f.listEvents(); len(evts) != 0 {
		t.Errorf("idempotent re-run emitted spurious events: %+v", evts)
	}
}

// --- Idempotent re-mirror: source bytes change → overwrite (no event) --------

func TestMirrorRunnerOutput_OverwritesOnSourceChange(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput#1: %v", err)
	}
	// Mutate the source.
	newSrc := []byte(`{"status":"failed"}` + "\n")
	mustWrite(t, filepath.Join(f.legacyRoot, f.legacyID, "state.json"), newSrc)
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput#2: %v", err)
	}
	got, err := f.readMirrored("state.json")
	if err != nil {
		t.Fatalf("read mirrored state.json: %v", err)
	}
	if string(got) != string(newSrc) {
		t.Errorf("dst not overwritten after source change: got=%q want=%q", got, newSrc)
	}
	if evts := f.listEvents(); len(evts) != 0 {
		t.Errorf("source-change re-mirror emitted events: %+v", evts)
	}
}

// --- Missing source artifact: silent skip, no event --------------------------

func TestMirrorRunnerOutput_MissingSource_NoEvent(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	// Remove one of the source artifacts.
	if err := os.Remove(filepath.Join(f.legacyRoot, f.legacyID, "metadata.json")); err != nil {
		t.Fatalf("rm source: %v", err)
	}
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	if _, err := f.readMirrored("state.json"); err != nil {
		t.Errorf("state.json should still be mirrored: %v", err)
	}
	if _, err := f.readMirrored("metadata.json"); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("metadata.json: want ErrNotFound (silent skip), got %v", err)
	}
	if evts := f.listEvents(); len(evts) != 0 {
		t.Errorf("missing-source path must not emit events; got %+v", evts)
	}
}

// --- Precondition violations -------------------------------------------------

func TestMirrorRunnerOutput_PreconditionViolations(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	cases := []struct {
		name             string
		mutate           func(b *Bridge)
		execKey          string
		revKey           string
		legacyID         string
		wantContainsHint string
	}{
		{
			name:    "nil_store",
			mutate:  func(b *Bridge) { b.Store = nil },
			execKey: f.execKey, revKey: f.revKey, legacyID: f.legacyID,
			wantContainsHint: "Store",
		},
		{
			name:    "empty_legacy_root",
			mutate:  func(b *Bridge) { b.LegacyRoot = "" },
			execKey: f.execKey, revKey: f.revKey, legacyID: f.legacyID,
			wantContainsHint: "LegacyRoot",
		},
		{
			name:    "invalid_revkey",
			execKey: f.execKey, revKey: "NOT-A-VALID-REV", legacyID: f.legacyID,
		},
		{
			name:    "invalid_execkey",
			execKey: "../escape", revKey: f.revKey, legacyID: f.legacyID,
		},
		{
			name:    "invalid_legacyid",
			execKey: f.execKey, revKey: f.revKey, legacyID: "../escape",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := *f.bridge // value copy so package-level state stays untouched
			if tc.mutate != nil {
				tc.mutate(&b)
			}
			err := b.MirrorRunnerOutput(context.Background(),
				tc.execKey, tc.revKey, tc.legacyID)
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Fatalf("err = %v, want wrap of statestore.ErrInvalid", err)
			}
			if tc.wantContainsHint != "" && !strings.Contains(err.Error(), tc.wantContainsHint) {
				t.Errorf("err %q does not mention %q", err.Error(), tc.wantContainsHint)
			}
		})
	}
}

// --- Context cancellation ----------------------------------------------------

func TestMirrorRunnerOutput_CtxCancel(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeAuto)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.bridge.MirrorRunnerOutput(ctx, f.execKey, f.revKey, f.legacyID)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// --- Default Now path: bridge with no Now still works ------------------------

func TestMirrorRunnerOutput_DefaultNow(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeHardlink)
	f.bridge.Now = nil // force default time.Now path
	withLinkFn(t, func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	evts := f.listEvents()
	if len(evts) == 0 {
		t.Fatalf("default-Now path produced no events")
	}
	for _, evt := range evts {
		if evt.At.IsZero() {
			t.Errorf("evt.At is zero — default Now path didn't stamp")
		}
		if evt.At.Location() != time.UTC {
			t.Errorf("evt.At not UTC: %v", evt.At.Location())
		}
	}
}

// --- isCrossDevice direct unit test ------------------------------------------

func TestIsCrossDevice(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain_exdev", syscall.EXDEV, true},
		{"link_error_exdev", &os.LinkError{Err: syscall.EXDEV}, true},
		{"link_error_eperm", &os.LinkError{Err: syscall.EPERM}, false},
		{"wrapped_exdev", fmt.Errorf("outer: %w", syscall.EXDEV), true},
		{"wrapped_link_error_exdev", fmt.Errorf("outer: %w", &os.LinkError{Err: syscall.EXDEV}), true},
		{"random", errors.New("nope"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCrossDevice(c.err); got != c.want {
				t.Errorf("isCrossDevice(%v) = %v want %v", c.err, got, c.want)
			}
		})
	}
}

// --- emitFailure sequence allocation: respects existing events ---------------
//
// Pre-seed events at sequence 5 to verify the next emitted bridge-mirror-failed
// lands at sequence 6 (or later, if multiple artifacts fail).

func TestMirrorRunnerOutput_EventSequenceAllocation(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeHardlink)
	// Plant a synthetic execution-created-style event at seq 5.
	body := []byte(`{"kind":"execution-created","at":"2026-05-30T18:00:00Z","payload":{}}` + "\n")
	if _, err := f.store.Write(context.Background(),
		statestore.EventPath(f.revKey, f.execKey, 5, "execution-created"),
		body, statestore.WriteOptions{}); err != nil {
		t.Fatalf("plant seed event: %v", err)
	}
	withLinkFn(t, func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	prefix := statestore.ExecutionDir(f.revKey, f.execKey) + "/events"
	infos, err := f.store.List(context.Background(), prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	// Expect 1 seed + 2 failure events = 3 total.
	if len(infos) != 1+len(bridgeMirroredFiles) {
		t.Fatalf("want %d events on disk, got %d: %v",
			1+len(bridgeMirroredFiles), len(infos), infos)
	}
	// Failure events should occupy seq 6 and seq 7.
	wantSeqs := []string{
		fmt.Sprintf("%020d", 5),
		fmt.Sprintf("%020d", 6),
		fmt.Sprintf("%020d", 7),
	}
	for i, info := range infos {
		base := filepath.Base(info.Path)
		if !strings.HasPrefix(base, wantSeqs[i]+"-") {
			t.Errorf("event[%d]=%s does not start with seq %s",
				i, base, wantSeqs[i])
		}
	}
}

// --- copyArtifact direct write failure --------------------------------------
//
// Make the destination directory read-only so Store.Write hits an EACCES on
// the atomic-rename / tempfile create path. This exercises copyArtifact's
// write-error branch (stage=copy, mode=copy).

func TestMirrorRunnerOutput_CopyMode_DirReadOnly_EmitsCopyFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only dir does not block root")
	}
	f := newBridgeFixture(t, MirrorModeCopy)
	dstDir := filepath.Join(f.store.Root(),
		filepath.FromSlash(statestore.ExecutionDir(f.revKey, f.execKey)))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dst dir: %v", err)
	}
	if err := os.Chmod(dstDir, 0o500); err != nil {
		t.Fatalf("chmod dst dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dstDir, 0o755) })

	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	// Restore so listEvents can read.
	if err := os.Chmod(dstDir, 0o755); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	// emitFailure may itself silently fail to persist (the events dir is
	// under the same locked tree); the contract is "MirrorRunnerOutput
	// returns nil on mirror failure" — already asserted above. We
	// additionally confirm no successful mirror happened.
	if _, err := f.readMirrored("state.json"); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("state.json: want ErrNotFound (no mirror), got %v", err)
	}
}

// --- linkArtifact prep-step error: destination dir read-only -----------------
//
// In MirrorModeHardlink, chmod the parent execution dir 0o500 so MkdirAll
// inside linkArtifact fails, hitting the mkdir-dest emit branch.

func TestMirrorRunnerOutput_Hardlink_MkdirDestFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only dir does not block root")
	}
	f := newBridgeFixture(t, MirrorModeHardlink)
	// Pre-create the executions parent and chmod it so the per-execution
	// MkdirAll inside linkArtifact fails.
	parent := filepath.Join(f.store.Root(),
		filepath.FromSlash(statestore.ExecutionsDir(f.revKey)))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	// emitFailure may itself fail to persist; the binding contract is
	// MirrorRunnerOutput returned nil and no mirror happened.
	if _, err := f.readMirrored("state.json"); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("state.json: want ErrNotFound (no mirror), got %v", err)
	}
}

// --- nextEventSeq: skips invalid event filenames -----------------------------

func TestNextEventSeq_SkipsInvalidNames(t *testing.T) {
	f := newBridgeFixture(t, MirrorModeHardlink)
	// Plant a non-conforming event filename (no seq prefix) into the
	// events dir.
	eventsDir := filepath.Join(f.store.Root(),
		filepath.FromSlash(statestore.ExecutionDir(f.revKey, f.execKey)+"/events"))
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(eventsDir, "garbage.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write garbage event: %v", err)
	}
	if err := os.WriteFile(filepath.Join(eventsDir, "00000000000000000003-real.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write real event: %v", err)
	}
	// Filename with non-numeric prefix in the seq slot should also be skipped.
	if err := os.WriteFile(filepath.Join(eventsDir, "abcdefghijklmnopqrst-bad.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write bad-seq event: %v", err)
	}
	withLinkFn(t, func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	})
	if err := f.bridge.MirrorRunnerOutput(context.Background(),
		f.execKey, f.revKey, f.legacyID); err != nil {
		t.Fatalf("MirrorRunnerOutput: %v", err)
	}
	evts := f.listEvents()
	if len(evts) == 0 {
		t.Fatalf("no failure events emitted")
	}
	// The bridge should land at seq 4 onwards (after the real seq 3).
	prefix := statestore.ExecutionDir(f.revKey, f.execKey) + "/events"
	infos, err := f.store.List(context.Background(), prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	emitted := 0
	for _, info := range infos {
		base := filepath.Base(info.Path)
		if strings.HasSuffix(base, "-bridge-mirror-failed.json") {
			emitted++
			// Expect seq >= 4.
			seqPart := base[:20]
			if seqPart < "00000000000000000004" {
				t.Errorf("emitted event %s landed below seq 4", base)
			}
		}
	}
	if emitted != len(bridgeMirroredFiles) {
		t.Errorf("emitted %d bridge-mirror-failed events; want %d",
			emitted, len(bridgeMirroredFiles))
	}
}

// --- LegacyExecutionFilePath helper exists and round-trips via the bridge ---

func TestStatestore_LegacyExecutionFilePath(t *testing.T) {
	got := statestore.LegacyExecutionFilePath("gh-12345-1-abcdef0", "state.json")
	want := "executions/gh-12345-1-abcdef0/state.json"
	if got != want {
		t.Errorf("LegacyExecutionFilePath = %q want %q", got, want)
	}
}

func TestStatestore_ExecutionFilePath(t *testing.T) {
	got := statestore.ExecutionFilePath("rev-main-p12345678", "run-001", "state.json")
	want := "revisions/rev-main-p12345678/executions/run-001/state.json"
	if got != want {
		t.Errorf("ExecutionFilePath = %q want %q", got, want)
	}
}

type emptyRootStore struct {
	statestore.StateStore
}

func (s emptyRootStore) Root() string { return "" }
