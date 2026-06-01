package executionstate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
)

// MirrorMode selects the bridge's per-call mirror strategy.
//
// MirrorModeAuto is the default and matches the M4 "Done when" contract
// (implementation-plan.md §M4): try hardlink first; on a cross-device
// (EXDEV) link error, transparently fall back to a byte-level copy. The
// design.md §11 risk register row "Hardlink mirror fails on cross-device
// FS" explicitly authorizes this fallback.
//
// MirrorModeHardlink forces a hardlink and surfaces a non-EXDEV link
// error via the failure event; an EXDEV result is also a failure (no
// fallback). Useful for tests and for callers that actively want to
// learn about cross-device drift.
//
// MirrorModeCopy forces a byte-level copy through the StateStore Write
// path, bypassing os.Link entirely. Useful for remote drivers (M5+) and
// for tests that want to exercise the copy code without faulting EXDEV.
type MirrorMode int

const (
	// MirrorModeAuto = hardlink-with-copy-fallback. Zero value so a
	// freshly constructed Bridge{} behaves correctly.
	MirrorModeAuto MirrorMode = iota
	// MirrorModeHardlink = hardlink only; any link error is a mirror
	// failure.
	MirrorModeHardlink
	// MirrorModeCopy = always copy via StateStore.Write.
	MirrorModeCopy
)

// String renders MirrorMode for diagnostics. Returned values are stable
// because they leak into the bridge-mirror-failed event payload's
// `mode` field (data-model.md §9 leaves the payload field set open;
// PR-B fixes the names here).
func (m MirrorMode) String() string {
	switch m {
	case MirrorModeAuto:
		return "auto"
	case MirrorModeHardlink:
		return "hardlink"
	case MirrorModeCopy:
		return "copy"
	default:
		return fmt.Sprintf("mirror-mode-%d", int(m))
	}
}

// bridgeMirroredFiles is the fixed set of legacy artifacts the bridge
// promotes into the revision-first execution directory. design.md §6
// marks state.json and metadata.json as `# bridge-mirrored`. Adding new
// names here without a spec amendment is intentionally a code change so
// reviewers spot the drift.
var bridgeMirroredFiles = []string{"state.json", "metadata.json"}

// Bridge mirrors a runner's legacy on-disk execution directory
// (`.orun/executions/<legacyExecID>/`) into the new revision-first
// layout (`revisions/<revKey>/executions/<execKey>/`) without disturbing
// either side. The struct shape is frozen by implementation-plan.md
// §M4 ("Bridge{Store, LegacyRoot, MirrorMode}").
//
// Bridge is safe for concurrent use across distinct (revKey, execKey)
// pairs; concurrent calls against the same target rely on
// StateStore.CreateIfAbsent and the mirror's read-then-write idempotency
// to converge.
type Bridge struct {
	// Store is the destination StateStore. Required. The bridge
	// resolves destination filesystem paths via Store.Root() so the
	// hardlink seam works against the local driver; remote drivers
	// (M5+) will set MirrorMode = MirrorModeCopy and route the bytes
	// through Store.Write.
	Store statestore.StateStore

	// LegacyRoot is the absolute filesystem path of the legacy
	// `.orun/executions` directory the runner writes into. The bridge
	// reads <LegacyRoot>/<legacyExecID>/<state.json|metadata.json>
	// from this prefix. Required.
	LegacyRoot string

	// MirrorMode selects the strategy used for each artifact mirror.
	// Zero value is MirrorModeAuto.
	MirrorMode MirrorMode

	// Now stamps the `at` field of any emitted bridge-mirror-failed
	// event. Same shape as Config.Now — when nil, time.Now().UTC is
	// used. Mirrors the PR-A clock shim; no new abstraction.
	Now func() time.Time

	// CatalogParent, when both keys are non-empty, causes the bridge to
	// also mirror state.json/metadata.json under the catalog-parent layout
	// sources/<SourceKey>/catalogs/<CatalogKey>/revisions/<revKey>/executions/<execKey>/
	// per implementation-plan.md C7. When either key is empty the
	// catalog-parent mirror is skipped.
	CatalogParent revision.CatalogParentRef
}

// linkFn is the test seam for os.Link. Production code links via
// os.Link; bridge_test.go swaps in a stub returning a *os.LinkError
// wrapping syscall.EXDEV so the cross-device fallback path is reachable
// on macOS/Linux CI without privileged FS mounts (implementation-plan.md
// §M4 "use a temp dir on a single FS plus a forced-EXDEV injection").
//
// Tests MUST restore the original via t.Cleanup; concurrent test
// execution is fine because every test that swaps linkFn also runs
// without t.Parallel — see bridge_test.go.
var linkFn = os.Link

// MirrorRunnerOutput mirrors the legacy state.json + metadata.json
// artifacts for legacyExecID into the new revision-first execution
// directory at revKey/execKey. The bridge tries os.Link first (subject
// to MirrorMode) and falls back to a byte-level copy on cross-device
// errors per design.md §11.
//
// Failure semantics (implementation-plan.md §M4):
//
//   - Precondition violations (nil Store, empty LegacyRoot, invalid
//     keys) return an error wrapping statestore.ErrInvalid. The bridge
//     is not invoked at all in this case.
//   - Mirror failures (link error in MirrorModeHardlink, copy error,
//     EXDEV when MirrorModeCopy already routes through Write, etc.)
//     emit a `bridge-mirror-failed` event under the execution's event
//     log and return nil. The resolver (PR-A) prefers the new layout
//     with a legacy fallback, so a missed mirror does not corrupt
//     `orun status`.
//
// Missing-source handling: if the legacy artifact does not exist, it is
// silently skipped — the runner is not required to write either file
// before calling the bridge, and emitting a failure event for a benign
// absence would create false-positive noise in the event log.
//
// Idempotent re-call: if the destination already contains byte-identical
// content, the call is a no-op for that artifact (no link, no copy, no
// event). A re-mirror with different source bytes overwrites via Write
// (the destination is unlinked first when hardlinking so os.Link does
// not fail with EEXIST).
func (b *Bridge) MirrorRunnerOutput(ctx context.Context, execKey, revKey, legacyExecID string) error {
	if b.Store == nil {
		return fmt.Errorf("%w: Bridge.Store is nil", statestore.ErrInvalid)
	}
	if b.LegacyRoot == "" {
		return fmt.Errorf("%w: Bridge.LegacyRoot is empty", statestore.ErrInvalid)
	}
	if err := revision.ValidateRevisionKey(revKey); err != nil {
		return err
	}
	if err := statestore.ValidateComponent(execKey); err != nil {
		return err
	}
	if err := statestore.ValidateComponent(legacyExecID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	for _, name := range bridgeMirroredFiles {
		if err := b.mirrorOne(ctx, revKey, execKey, legacyExecID, name); err != nil {
			// mirrorOne already emitted the failure event; surface
			// nil per the M4 contract so callers (runner) keep
			// progressing the execution.
			_ = err
		}
	}

	// Catalog-parent dual-write (C7). After mirroring to the Phase 1
	// layout above, also copy state.json/metadata.json under the
	// catalog-parent execution dir. Best-effort — errors logged, not
	// propagated. Uses Store.Write (copy mode) since the source bytes
	// are already in memory from the legacy read.
	if b.CatalogParent.Active() {
		b.mirrorToCatalogParent(ctx, revKey, execKey, legacyExecID)
	}

	return nil
}

// MirrorRunnerLog mirrors one legacy step log into the revision-first and,
// when configured, catalog-parent execution directories. It is best-effort
// like MirrorRunnerOutput: malformed preconditions are returned, while missing
// or failed artifact writes are skipped so runner execution is never blocked.
func (b *Bridge) MirrorRunnerLog(ctx context.Context, execKey, revKey, legacyExecID, jobID, stepID string) error {
	if b.Store == nil {
		return fmt.Errorf("%w: Bridge.Store is nil", statestore.ErrInvalid)
	}
	if b.LegacyRoot == "" {
		return fmt.Errorf("%w: Bridge.LegacyRoot is empty", statestore.ErrInvalid)
	}
	if err := revision.ValidateRevisionKey(revKey); err != nil {
		return err
	}
	if err := statestore.ValidateComponent(execKey); err != nil {
		return err
	}
	if err := statestore.ValidateComponent(legacyExecID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	srcJobSeg := legacyRunnerLogSegment(jobID)
	srcStepSeg := legacyRunnerLogSegment(stepID)
	jobSeg := runnerLogSegment(jobID)
	stepSeg := runnerLogSegment(stepID)
	srcAbs := filepath.Join(b.LegacyRoot, legacyExecID, "logs", srcJobSeg, srcStepSeg+".log")
	srcBytes, err := os.ReadFile(srcAbs)
	if err != nil {
		return nil
	}
	globalPath := statestore.ExecutionDir(revKey, execKey) + "/logs/" + jobSeg + "/" + stepSeg + ".log"
	_, _ = b.Store.Write(ctx, globalPath, srcBytes, statestore.WriteOptions{})
	if b.CatalogParent.Active() {
		if dir, derr := catalogstore.CatalogExecutionDir(b.CatalogParent.SourceKey, b.CatalogParent.CatalogKey, revKey, execKey); derr == nil {
			_, _ = b.Store.Write(ctx, dir+"/logs/"+jobSeg+"/"+stepSeg+".log", srcBytes, statestore.WriteOptions{})
		}
	}
	return nil
}

// mirrorOne mirrors a single artifact. It returns a non-nil error only
// to short-circuit MirrorRunnerOutput's loop after a failure event has
// already been emitted; callers MUST treat the return value as
// best-effort diagnostic.
func (b *Bridge) mirrorOne(ctx context.Context, revKey, execKey, legacyExecID, name string) error {
	srcAbs := filepath.Join(b.LegacyRoot, legacyExecID, name)
	srcBytes, srcErr := os.ReadFile(srcAbs)
	if srcErr != nil {
		if errors.Is(srcErr, fs.ErrNotExist) {
			// Benign: legacy artifact not produced. No event.
			return nil
		}
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "read-source", srcErr)
		return srcErr
	}

	dstLogical := statestore.ExecutionFilePath(revKey, execKey, name)

	// Idempotent short-circuit: identical bytes already at the
	// destination → no-op, no event. This is the contract the
	// "idempotent re-mirror" test exercises.
	if existing, _, err := b.Store.Read(ctx, dstLogical); err == nil {
		if equalBytes(existing, srcBytes) {
			return nil
		}
	} else if !errors.Is(err, statestore.ErrNotFound) {
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "read-dest", err)
		return err
	}

	switch b.MirrorMode {
	case MirrorModeCopy:
		return b.copyArtifact(ctx, revKey, execKey, legacyExecID, name, dstLogical, srcBytes)
	case MirrorModeHardlink:
		return b.linkArtifact(ctx, revKey, execKey, legacyExecID, name, srcAbs, dstLogical, srcBytes, false /*allowFallback*/)
	case MirrorModeAuto:
		fallthrough
	default:
		return b.linkArtifact(ctx, revKey, execKey, legacyExecID, name, srcAbs, dstLogical, srcBytes, true /*allowFallback*/)
	}
}

// linkArtifact attempts os.Link via the linkFn seam. On EXDEV with
// allowFallback, it routes through copyArtifact. On any other error,
// it emits a bridge-mirror-failed event (in MirrorModeHardlink an
// EXDEV is also a failure — the caller asked for hardlink-only).
func (b *Bridge) linkArtifact(
	ctx context.Context,
	revKey, execKey, legacyExecID, name string,
	srcAbs, dstLogical string,
	srcBytes []byte,
	allowFallback bool,
) error {
	dstAbs, err := b.destAbs(dstLogical)
	if err != nil {
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "translate-dest", err)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "mkdir-dest", err)
		return err
	}
	// Remove any pre-existing destination so os.Link does not fail
	// with EEXIST. The earlier identical-bytes short-circuit already
	// handled the no-op case.
	if err := os.Remove(dstAbs); err != nil && !errors.Is(err, fs.ErrNotExist) {
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "remove-dest", err)
		return err
	}
	if err := linkFn(srcAbs, dstAbs); err != nil {
		if isCrossDevice(err) && allowFallback {
			return b.copyArtifact(ctx, revKey, execKey, legacyExecID, name, dstLogical, srcBytes)
		}
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "link", err)
		return err
	}
	return nil
}

// copyArtifact writes srcBytes through Store.Write, taking advantage of
// the StateStore's atomic-replace contract (state-store.md §6).
func (b *Bridge) copyArtifact(
	ctx context.Context,
	revKey, execKey, legacyExecID, name, dstLogical string,
	srcBytes []byte,
) error {
	if _, err := b.Store.Write(ctx, dstLogical, srcBytes, statestore.WriteOptions{}); err != nil {
		b.emitFailure(ctx, revKey, execKey, legacyExecID, name, "copy", err)
		return err
	}
	return nil
}

// destAbs translates a logical path into an absolute filesystem path
// under Store.Root(). The translation is intentionally simple: the
// StateStore contract documents Root() as diagnostic, but the bridge is
// a local-driver-only construct (hardlinks are filesystem-bound), and
// remote-driver callers select MirrorModeCopy which never reaches this
// function.
func (b *Bridge) destAbs(logical string) (string, error) {
	root := b.Store.Root()
	if root == "" {
		return "", fmt.Errorf("%w: bridge: Store.Root() is empty", statestore.ErrInvalid)
	}
	return filepath.Join(root, filepath.FromSlash(logical)), nil
}

// isCrossDevice reports whether err is a cross-device link error. The
// detection is portable across darwin/linux: errors.Is handles wrapped
// chains, and the *os.LinkError type-assertion covers the common
// shape returned by os.Link. String sniffing is forbidden by the M4
// constraints.
func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return false
}

// bridgeMirrorFailedEvent encodes the data-model.md §9
// `bridge-mirror-failed` event entry. The payload field set is
// stable (data-model.md §9 leaves it open; PR-B fixes the names so
// downstream readers — `orun status`, future metrics — have a contract).
//
// Adding fields is non-breaking; renaming or removing fields is.
type bridgeMirrorFailedEvent struct {
	Kind    string                    `json:"kind"`
	At      time.Time                 `json:"at"`
	Payload bridgeMirrorFailedPayload `json:"payload"`
}

type bridgeMirrorFailedPayload struct {
	ExecutionKey string `json:"executionKey"`
	RevisionKey  string `json:"revisionKey"`
	LegacyExecID string `json:"legacyExecId"`
	Artifact     string `json:"artifact"`
	Stage        string `json:"stage"`
	Mode         string `json:"mode"`
	Error        string `json:"error"`
}

// emitFailure appends a bridge-mirror-failed event under the
// execution's event log. It is best-effort: a failure to write the
// failure event is dropped (logging or metric integration is M5+).
//
// The next sequence is derived by scanning events/ via Store.List,
// which already round-trips the trigger-first layout. CreateIfAbsent
// guards against concurrent writers; on conflict, the loop bumps to
// seq+1 until a free slot is found, bounded by mirrorEventRetryBudget.
func (b *Bridge) emitFailure(
	ctx context.Context,
	revKey, execKey, legacyExecID, artifact, stage string,
	cause error,
) {
	now := b.now()
	evt := bridgeMirrorFailedEvent{
		Kind: "bridge-mirror-failed",
		At:   now,
		Payload: bridgeMirrorFailedPayload{
			ExecutionKey: execKey,
			RevisionKey:  revKey,
			LegacyExecID: legacyExecID,
			Artifact:     artifact,
			Stage:        stage,
			Mode:         b.MirrorMode.String(),
			Error:        cause.Error(),
		},
	}
	body := marshalCanonicalJSON(evt)

	seq, err := b.nextEventSeq(ctx, revKey, execKey)
	if err != nil {
		return
	}
	for i := 0; i < mirrorEventRetryBudget; i++ {
		evtPath := statestore.EventPath(revKey, execKey, seq, "bridge-mirror-failed")
		_, werr := b.Store.CreateIfAbsent(ctx, evtPath, body)
		if werr == nil {
			return
		}
		if !errors.Is(werr, statestore.ErrExists) {
			return
		}
		seq++
	}
}

// mirrorEventRetryBudget bounds the next-seq retry loop in emitFailure.
// 32 attempts is enough headroom for any plausible concurrent burst
// while still bounding pathological live-locks.
const mirrorEventRetryBudget = 32

// now returns the bridge's current time, defaulting to time.Now().UTC()
// when Now is unset. Mirrors revision.Config.resolveDefaults / writer's
// Config.resolveDefaults.
func (b *Bridge) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// nextEventSeq returns the next event sequence number for the given
// execution. Sequence 1 is reserved for the execution-created event
// (writer.go), so a freshly created execution returns 2.
func (b *Bridge) nextEventSeq(ctx context.Context, revKey, execKey string) (uint64, error) {
	prefix := statestore.ExecutionDir(revKey, execKey) + "/events"
	infos, err := b.Store.List(ctx, prefix)
	if err != nil {
		return 0, err
	}
	maxSeq := uint64(0)
	for _, info := range infos {
		base := pathBase(info.Path)
		// Filenames are <020d>-<kind>.json; the leading 20 bytes
		// are the zero-padded sequence.
		if len(base) < 21 || base[20] != '-' {
			continue
		}
		n, perr := strconv.ParseUint(strings.TrimLeft(base[:20], "0"), 10, 64)
		if perr != nil {
			// "00000000000000000000" → empty after trim → treat as 0.
			if strings.TrimLeft(base[:20], "0") == "" {
				n = 0
			} else {
				continue
			}
		}
		if n > maxSeq {
			maxSeq = n
		}
	}
	if maxSeq == 0 {
		// No prior events; the bridge still starts at 2 to leave
		// seq 1 reserved for execution-created.
		return 2, nil
	}
	return maxSeq + 1, nil
}

// mirrorToCatalogParent copies each bridgeMirroredFile from the legacy
// execution directory to the catalog-parent execution path. Best-effort:
// a failure on any artifact is silently swallowed so the run is not
// blocked by a catalog-write issue.
func (b *Bridge) mirrorToCatalogParent(ctx context.Context, revKey, execKey, legacyExecID string) {
	for _, name := range bridgeMirroredFiles {
		srcAbs := filepath.Join(b.LegacyRoot, legacyExecID, name)
		srcBytes, err := os.ReadFile(srcAbs)
		if err != nil {
			continue
		}
		dstLogical, err := catalogstore.CatalogExecutionFilePath(
			b.CatalogParent.SourceKey, b.CatalogParent.CatalogKey,
			revKey, execKey, name)
		if err != nil {
			continue
		}
		_, _ = b.Store.Write(ctx, dstLogical, srcBytes, statestore.WriteOptions{})
	}
}

func runnerLogSegment(s string) string {
	s = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "unknown"
	}
	return out
}

func legacyRunnerLogSegment(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(s)
}
