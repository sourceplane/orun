package continuity

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeState is the control plane's state routes for one session and its
// predecessor, in memory.
type fakeState struct {
	mu        sync.Mutex
	lines     map[int]string // what the session posted, by seq
	meta      map[string]any
	parts     map[int][]byte
	failNext  int
	authSeen  []string
	resume    any      // the /state/resume answer's `resume`
	prevLines []string // the predecessor's transcript
	prevSnap  []byte
	pageSize  int
}

func newFake() *fakeState {
	return &fakeState{lines: map[int]string{}, meta: map[string]any{}, parts: map[int][]byte{}, pageSize: 2}
}

func (f *fakeState) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.authSeen = append(f.authSeen, r.Header.Get("Authorization"))
		data := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
		}
		if f.failNext > 0 {
			f.failNext--
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/state/transcript":
			var body struct {
				HarnessSession string `json:"harnessSession"`
				ProjectDir     string `json:"projectDir"`
				Lines          []struct {
					Seq  int    `json:"seq"`
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("transcript body: %v", err)
			}
			for _, l := range body.Lines {
				f.lines[l.Seq] = l.Text
			}
			f.meta["harnessSession"] = body.HarnessSession
			data(map[string]any{"stored": len(body.Lines)})
		case r.Method == http.MethodPut && r.URL.Path == "/state/meta":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			for k, v := range m {
				f.meta[k] = v
			}
			data(f.meta)
		case r.Method == http.MethodPut && r.URL.Path == "/state/snapshot":
			part, _ := strconv.Atoi(r.URL.Query().Get("part"))
			b, _ := io.ReadAll(r.Body)
			f.parts[part] = b
			data(map[string]any{"complete": true})
		case r.Method == http.MethodGet && r.URL.Path == "/state/resume":
			data(map[string]any{"resume": f.resume})
		case r.Method == http.MethodGet && r.URL.Path == "/state/resume/transcript":
			after, _ := strconv.Atoi(r.URL.Query().Get("after"))
			var page []map[string]any
			var next any
			for i := after + 1; i < len(f.prevLines); i++ {
				if len(page) == f.pageSize {
					next = i - 1
					break
				}
				page = append(page, map[string]any{"seq": i, "text": f.prevLines[i]})
			}
			data(map[string]any{"harnessSession": "hs-1", "lines": page, "next": next})
		case r.Method == http.MethodGet && r.URL.Path == "/state/resume/snapshot":
			part, _ := strconv.Atoi(r.URL.Query().Get("part"))
			lo, hi := part*snapshotPartBytes, (part+1)*snapshotPartBytes
			if hi > len(f.prevSnap) {
				hi = len(f.prevSnap)
			}
			if lo >= len(f.prevSnap) {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(f.prevSnap[lo:hi])
		default:
			http.NotFound(w, r)
		}
	})
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a work tree with one commit: a.txt, b.txt and a .gitignore that
// ignores .env.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run(t, dir, "init", "--quiet", "--initial-branch=main")
	run(t, dir, "config", "user.email", "t@example.com")
	run(t, dir, "config", "user.name", "T")
	for name, body := range map[string]string{"a.txt": "a\n", "b.txt": "b\n", ".gitignore": ".env\nnode_modules/\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "--quiet", "-m", "seed")
	return dir
}

func TestProjectDirMatchesClaudeCode(t *testing.T) {
	if got := ProjectDir("/home/orun/work/photo-portfolio-db71df"); got != "-home-orun-work-photo-portfolio-db71df" {
		t.Errorf("ProjectDir = %q", got)
	}
	if got := ProjectDir("/tmp/a.b/c_d"); got != "-tmp-a-b-c-d" {
		t.Errorf("ProjectDir = %q", got)
	}
}

func TestKeeperPostsCompleteLinesInOrder(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	claude := t.TempDir()
	work := t.TempDir()
	cfg := Config{BaseURL: srv.URL, Token: func() string { return "ast_tok" }, Workdir: work, ClaudeDir: claude}
	k := NewKeeper(cfg)
	dir := filepath.Join(claude, "projects", ProjectDir(work))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "hs-1.jsonl")

	// Before the harness names its session, nothing is read.
	if err := os.WriteFile(file, []byte(`{"n":0}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k.syncTranscript(context.Background())
	if len(f.lines) != 0 {
		t.Fatalf("posted before init: %v", f.lines)
	}
	k.Observe("harness_event", map[string]any{"phase": "init", "harnessSession": "hs-1"})

	// A line still being written waits.
	appendTo(t, file, `{"n":1}`+"\n"+`{"n":2,"partial`)
	k.syncTranscript(context.Background())
	if want := map[int]string{0: `{"n":0}`, 1: `{"n":1}`}; !equalLines(f.lines, want) {
		t.Fatalf("lines = %v, want %v", f.lines, want)
	}
	appendTo(t, file, `":true}`+"\n"+`{"n":3}`+"\n")
	k.syncTranscript(context.Background())
	if want := map[int]string{0: `{"n":0}`, 1: `{"n":1}`, 2: `{"n":2,"partial":true}`, 3: `{"n":3}`}; !equalLines(f.lines, want) {
		t.Fatalf("lines = %v, want %v", f.lines, want)
	}
	if f.meta["projectDir"] != ProjectDir(work) || f.meta["harnessSession"] != "hs-1" {
		t.Errorf("meta = %v", f.meta)
	}
	for _, a := range f.authSeen {
		if a != "Bearer ast_tok" {
			t.Fatalf("authorization = %q", a)
		}
	}
}

func TestKeeperRetriesWhatTheServerRefused(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	claude, work := t.TempDir(), t.TempDir()
	k := NewKeeper(Config{BaseURL: srv.URL, Workdir: work, ClaudeDir: claude})
	dir := filepath.Join(claude, "projects", ProjectDir(work))
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "hs-1.jsonl"), []byte("{\"n\":0}\n{\"n\":1}\n"), 0o600)
	k.Observe("harness_event", map[string]any{"harnessSession": "hs-1"})
	k.metaSent = "hs-1" // isolate the transcript call
	f.failNext = 1
	k.syncTranscript(context.Background())
	if len(f.lines) != 0 {
		t.Fatalf("lines after a refusal = %v", f.lines)
	}
	k.syncTranscript(context.Background())
	if len(f.lines) != 2 || f.lines[1] != `{"n":1}` {
		t.Fatalf("lines after the retry = %v", f.lines)
	}
}

func TestSnapshotCarriesUnpushedWorkAndNothingIgnored(t *testing.T) {
	dir := repo(t)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new", "c.txt"), nil, 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "new"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "new", "c.txt"), []byte("c\n"), 0o644)
	_ = os.Remove(filepath.Join(dir, "b.txt"))
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.js"), []byte("x\n"), 0o644)

	b, err := buildSnapshot(context.Background(), dir)
	if err != nil || b == nil {
		t.Fatalf("buildSnapshot = %v, %v", b, err)
	}
	names := tarNames(t, b.data)
	for _, want := range []string{"a.txt", "new/c.txt", deletedEntry} {
		if !names[want] {
			t.Errorf("snapshot lacks %s (has %v)", want, names)
		}
	}
	for _, never := range []string{".env", "node_modules/x/i.js"} {
		if names[never] {
			t.Errorf("snapshot carries %s", never)
		}
	}
	if len(b.headSha) != 40 || b.branch != "main" {
		t.Errorf("head = %q branch = %q", b.headSha, b.branch)
	}

	// Applied to a clean copy at the same commit, it brings the work back.
	clean := t.TempDir()
	run(t, clean, "clone", "--quiet", dir, ".")
	n, err := applySnapshot(clean, b.data)
	if err != nil || n != 2 {
		t.Fatalf("applySnapshot = %d, %v", n, err)
	}
	if got, _ := os.ReadFile(filepath.Join(clean, "a.txt")); string(got) != "a changed\n" {
		t.Errorf("a.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(clean, "new", "c.txt")); string(got) != "c\n" {
		t.Errorf("new/c.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(clean, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("b.txt still there after the snapshot deleted it")
	}

	// A clean working copy has nothing to snapshot.
	if b2, err := buildSnapshot(context.Background(), clean+"/../"+filepath.Base(clean)); err != nil {
		t.Fatal(err)
	} else if b2 == nil {
		t.Errorf("a changed copy produced no snapshot")
	}
	pristine := t.TempDir()
	run(t, pristine, "clone", "--quiet", dir, ".")
	if b3, err := buildSnapshot(context.Background(), pristine); err != nil || b3 != nil {
		t.Errorf("a clean copy = %v, %v", b3, err)
	}
}

func TestApplySnapshotTouchesNothingOutsideTheWorkdir(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"../escape.txt", "/abs.txt", "ok.txt", "node_modules/m.js"} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x\n"))
	}
	_ = tw.Close()
	_ = gz.Close()
	root := t.TempDir()
	dir := filepath.Join(root, "work")
	_ = os.MkdirAll(dir, 0o755)
	n, err := applySnapshot(dir, buf.Bytes())
	if err != nil || n != 1 {
		t.Fatalf("applySnapshot = %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Errorf("a ../ entry was written outside the workdir")
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "m.js")); !os.IsNotExist(err) {
		t.Errorf("node_modules was written")
	}
}

func TestResumeRestoresTheConversationAndTheWork(t *testing.T) {
	prev := repo(t)
	_ = os.WriteFile(filepath.Join(prev, "a.txt"), []byte("unpushed\n"), 0o644)
	snap, err := buildSnapshot(context.Background(), prev)
	if err != nil || snap == nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	run(t, work, "clone", "--quiet", prev, ".") // the successor, at the same commit

	f := newFake()
	f.prevLines = []string{`{"type":"user","n":0}`, `{"type":"assistant","n":1}`, `{"type":"user","n":2}`, `{"type":"assistant","n":3}`, `{"type":"user","n":4}`}
	f.prevSnap = snap.data
	f.resume = map[string]any{
		"from":     "as_prev",
		"meta":     map[string]any{"harnessSession": "hs-1", "projectDir": "-home-orun-work-elsewhere", "transcriptLines": 4},
		"snapshot": map[string]any{"id": "1", "parts": 1, "bytes": len(snap.data), "headSha": snap.headSha, "branch": "main"},
	}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	claude := t.TempDir()
	got, err := Resume(context.Background(), Config{BaseURL: srv.URL, Workdir: work, ClaudeDir: claude})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.From != "as_prev" || got.HarnessSession != "hs-1" || got.Lines != 5 || got.SnapshotFiles != 1 || got.SnapshotSkipped != "" {
		t.Fatalf("Resume = %+v", got)
	}
	want := strings.Join(f.prevLines, "\n") + "\n"
	for _, d := range []string{ProjectDir(work), "-home-orun-work-elsewhere"} {
		body, err := os.ReadFile(filepath.Join(claude, "projects", d, "hs-1.jsonl"))
		if err != nil || string(body) != want {
			t.Errorf("transcript under %s = %q, %v", d, body, err)
		}
	}
	if body, _ := os.ReadFile(filepath.Join(work, "a.txt")); string(body) != "unpushed\n" {
		t.Errorf("a.txt = %q", body)
	}
}

func TestResumeLeavesASnapshotForAnotherCommitAlone(t *testing.T) {
	prev := repo(t)
	_ = os.WriteFile(filepath.Join(prev, "a.txt"), []byte("unpushed\n"), 0o644)
	snap, _ := buildSnapshot(context.Background(), prev)
	work := t.TempDir()
	run(t, work, "clone", "--quiet", prev, ".")
	run(t, work, "config", "user.email", "t@example.com")
	run(t, work, "config", "user.name", "T")
	_ = os.WriteFile(filepath.Join(work, "later.txt"), []byte("later\n"), 0o644)
	run(t, work, "add", ".")
	run(t, work, "commit", "--quiet", "-m", "pushed later")

	f := newFake()
	f.prevSnap = snap.data
	f.resume = map[string]any{"from": "as_prev", "meta": map[string]any{}, "snapshot": map[string]any{"id": "1", "parts": 1, "bytes": len(snap.data), "headSha": snap.headSha}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	got, err := Resume(context.Background(), Config{BaseURL: srv.URL, Workdir: work, ClaudeDir: t.TempDir()})
	if err != nil || got.SnapshotSkipped == "" || got.SnapshotFiles != 0 || got.HarnessSession != "" {
		t.Fatalf("Resume = %+v, %v", got, err)
	}
	if body, _ := os.ReadFile(filepath.Join(work, "a.txt")); string(body) != "a\n" {
		t.Errorf("a.txt = %q — a stale snapshot was applied", body)
	}
}

func TestResumeWithNothingToResume(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	got, err := Resume(context.Background(), Config{BaseURL: srv.URL, Workdir: t.TempDir(), ClaudeDir: t.TempDir()})
	if got != nil || err != nil {
		t.Fatalf("Resume = %+v, %v", got, err)
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func equalLines(a, b map[int]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range b {
		if a[k] != v {
			return false
		}
	}
	return true
}

func tarNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = true
	}
}
