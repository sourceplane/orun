package continuity

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Keeper keeps a running session's state: it tails Claude Code's transcript
// and posts new complete lines (every FlushEvery, and at each turn's end), and
// snapshots unpushed work at each turn's end and every SnapshotEvery while the
// working copy is dirty. It learns the harness's session id from the session
// log (Observe) — the harness names it in its `init`.
type Keeper struct {
	cfg Config

	mu             sync.Mutex
	harnessSession string
	turnEnded      bool

	// The tail's position, touched only by the Run goroutine.
	file       string
	projectDir string
	offset     int64
	seq        int
	lastDigest string
	metaSent   string

	wake chan struct{}
	done chan struct{}
}

// NewKeeper builds a keeper; Run starts it.
func NewKeeper(cfg Config) *Keeper {
	return &Keeper{cfg: cfg, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// Observe reads the session log as it is written: the harness's `init` (its
// session id) and each turn's end (a cost sample). It never blocks.
func (k *Keeper) Observe(kind string, payload map[string]any) {
	switch kind {
	case "harness_event":
		if id, _ := payload["harnessSession"].(string); id != "" && harnessIDRe.MatchString(id) {
			k.mu.Lock()
			k.harnessSession = id
			k.mu.Unlock()
			k.poke()
		}
	case "cost_sample":
		k.mu.Lock()
		k.turnEnded = true
		k.mu.Unlock()
		k.poke()
	}
}

func (k *Keeper) poke() {
	select {
	case k.wake <- struct{}{}:
	default:
	}
}

// Run keeps state until ctx ends, then makes one last bounded pass.
func (k *Keeper) Run(ctx context.Context) {
	defer close(k.done)
	tick := time.NewTicker(k.cfg.flushEvery())
	defer tick.Stop()
	snap := time.NewTicker(k.cfg.snapshotEvery())
	defer snap.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			k.syncTranscript(final)
			k.snapshot(final)
			cancel()
			return
		case <-tick.C:
			k.syncTranscript(ctx)
		case <-k.wake:
			k.syncTranscript(ctx)
			k.mu.Lock()
			ended := k.turnEnded
			k.turnEnded = false
			k.mu.Unlock()
			if ended {
				k.snapshot(ctx)
			}
		case <-snap.C:
			k.snapshot(ctx)
		}
	}
}

// Wait blocks until Run has made its last pass (or the timeout passes).
func (k *Keeper) Wait(timeout time.Duration) {
	select {
	case <-k.done:
	case <-time.After(timeout):
	}
}

// findTranscript locates <ClaudeDir>/projects/*/<id>.jsonl.
func (k *Keeper) findTranscript(id string) (string, string) {
	want := ProjectDir(k.cfg.Workdir)
	exact := filepath.Join(k.cfg.ClaudeDir, "projects", want, id+".jsonl")
	if _, err := os.Stat(exact); err == nil {
		return exact, want
	}
	matches, _ := filepath.Glob(filepath.Join(k.cfg.ClaudeDir, "projects", "*", id+".jsonl"))
	if len(matches) == 0 {
		return "", ""
	}
	return matches[0], filepath.Base(filepath.Dir(matches[0]))
}

// syncTranscript posts the transcript's new complete lines.
func (k *Keeper) syncTranscript(ctx context.Context) {
	k.mu.Lock()
	id := k.harnessSession
	k.mu.Unlock()
	if id == "" {
		return
	}
	if k.file == "" || !strings.HasSuffix(k.file, id+".jsonl") {
		f, dir := k.findTranscript(id)
		if f == "" {
			return // the harness has not written it yet
		}
		k.file, k.projectDir, k.offset, k.seq = f, dir, 0, 0
	}
	if k.metaSent != id {
		PutMeta(ctx, k.cfg, Meta{HarnessSession: id, ProjectDir: k.projectDir})
		k.metaSent = id
	}
	f, err := os.Open(k.file)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(k.offset, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil || len(data) == 0 {
		return
	}
	// Only complete lines: a line the harness is still writing waits.
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return
	}
	var lines []map[string]any
	var batchChars int
	consumed := 0
	flush := func() bool {
		if len(lines) == 0 {
			return true
		}
		body := map[string]any{"harnessSession": id, "projectDir": k.projectDir, "lines": lines}
		if err := k.cfg.request(ctx, http.MethodPost, "/state/transcript", body, nil); err != nil {
			k.cfg.logf("orun agent serve: continuity — transcript not kept yet (%v); retrying\n", err)
			return false
		}
		k.offset += int64(consumed)
		k.seq += len(lines)
		lines, batchChars, consumed = nil, 0, 0
		return true
	}
	start := 0
	for start <= last {
		end := bytes.IndexByte(data[start:], '\n') + start
		text := string(data[start:end])
		size := end - start + 1
		start = end + 1
		if strings.TrimSpace(text) == "" {
			consumed += size
			continue
		}
		if len(text) > maxBatchChars {
			// Larger than a batch may carry: kept as a marker so the numbering
			// and the successor's file stay line for line.
			k.cfg.logf("orun agent serve: continuity — a %d-byte transcript line is too long to keep; kept as a marker\n", len(text))
			text = `{"type":"system","subtype":"orun_continuity_omitted","bytes":` + strconv.Itoa(len(text)) + `}`
		}
		if len(lines) == 100 || batchChars+len(text) > maxBatchChars {
			if !flush() {
				return
			}
		}
		lines = append(lines, map[string]any{"seq": k.seq + len(lines), "text": text})
		batchChars += len(text)
		consumed += size
	}
	flush()
}

const maxBatchChars = 3_500_000

// snapshot uploads unpushed work when it changed since the last one.
func (k *Keeper) snapshot(ctx context.Context) {
	if k.cfg.Workdir == "" {
		return
	}
	b, err := buildSnapshot(ctx, k.cfg.Workdir)
	if err != nil {
		k.cfg.logf("orun agent serve: continuity — no snapshot (%v)\n", err)
		return
	}
	if b == nil || b.digest == k.lastDigest {
		return
	}
	if err := uploadSnapshot(ctx, k.cfg, b); err != nil {
		k.cfg.logf("orun agent serve: continuity — snapshot not kept (%v)\n", err)
		return
	}
	k.lastDigest = b.digest
}
