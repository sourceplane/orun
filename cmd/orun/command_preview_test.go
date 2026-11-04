package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/preview"
)

const previewServerHelperEnv = "ORUN_TEST_PREVIEW_SERVER"

// TestPreviewDevServerHelper is not a real test: TestPreviewCLI declares it
// as the repository's dev server, so the preview runs this binary.
func TestPreviewDevServerHelper(t *testing.T) {
	if os.Getenv(previewServerHelperEnv) == "" {
		t.Skip("helper process for TestPreviewCLI")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("PORT"))
	if err != nil {
		t.Fatal(err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}

const previewCLIHelperArgs = "ORUN_TEST_PREVIEW_CLI_ARGS"

// TestPreviewCLIHelper is not a real test: runOrun re-executes the test
// binary to run one orun command in a fresh process, with fresh CLI globals.
func TestPreviewCLIHelper(t *testing.T) {
	raw := os.Getenv(previewCLIHelperArgs)
	if raw == "" {
		t.Skip("helper process for the preview CLI tests")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("ORUN-ERROR: %v\n", err)
	}
}

// runOrun runs one orun command and returns what it printed, and its error
// as text ("" on success).
func runOrun(t *testing.T, args ...string) (out, errText string) {
	t.Helper()
	raw, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestPreviewCLIHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), previewCLIHelperArgs+"="+string(raw), autoRefreshEnvVar+"=1")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("orun %v: %v\n%s", args, err, b)
	}
	out = strings.TrimSuffix(string(b), "PASS\n")
	if i := strings.Index(out, "ORUN-ERROR: "); i >= 0 {
		out, errText = out[:i], strings.TrimSpace(out[i+len("ORUN-ERROR: "):])
	}
	return out, errText
}

// jsonState is the state object a --json command printed.
func jsonState(t *testing.T, out string) preview.State {
	t.Helper()
	var st preview.State
	start, end := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if start < 0 || end < start || json.Unmarshal([]byte(out[start:end+1]), &st) != nil {
		t.Fatalf("no state in:\n%s", out)
	}
	return st
}

func previewRepo(t *testing.T, port int) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("intent.yaml", "apiVersion: sourceplane.io/v1\nkind: Intent\nmetadata:\n  name: p\ndiscovery:\n  roots: [apps]\npreview:\n  component: web\n")
	run, _ := json.Marshal([]string{exe, "-test.run=^TestPreviewDevServerHelper$"})
	write("apps/web/component.yaml", "apiVersion: sourceplane.io/v1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
		"  preview:\n    run: "+string(run)+"\n    port: "+strconv.Itoa(port)+"\n    path: /app\n    env:\n      "+previewServerHelperEnv+": \"1\"\n")
	return repo
}

func TestPreviewCLI(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	repo := previewRepo(t, port)
	intent := filepath.Join(repo, "intent.yaml")
	t.Cleanup(func() { _, _ = preview.Stop(repo) })

	out, errText := runOrun(t, "preview", "start", "--intent", intent, "--json", "--timeout", "60s")
	if errText != "" {
		t.Fatalf("start: %s\n%s", errText, out)
	}
	st := jsonState(t, out)
	if st.State != preview.StateReady || st.Component != "web" || st.Port != port || st.Path != "/app" || len(st.Processes) != 1 {
		t.Fatalf("state = %+v", st)
	}

	// Bare `orun preview` is start, and leaves the running one alone.
	out, errText = runOrun(t, "preview", "--intent", intent, "--json")
	if again := jsonState(t, out); errText != "" || again.Processes[0].PID != st.Processes[0].PID {
		t.Fatalf("second start = %+v, %s", again, errText)
	}

	out, errText = runOrun(t, "preview", "status", "--intent", intent)
	if errText != "" || !strings.Contains(out, "preview ready — web on http://127.0.0.1:"+strconv.Itoa(port)+"/app") {
		t.Fatalf("status: %s\n%s", errText, out)
	}
	out, errText = runOrun(t, "preview", "logs", "--intent", intent)
	if errText != "" || !strings.Contains(out, "orun preview: web — ") {
		t.Fatalf("logs: %s\n%s", errText, out)
	}
	out, errText = runOrun(t, "preview", "stop", "--intent", intent)
	if errText != "" || !strings.Contains(out, "preview stopped (1 process(es))") {
		t.Fatalf("stop: %s\n%s", errText, out)
	}
	out, errText = runOrun(t, "preview", "status", "--intent", intent, "--json")
	if errText != "" || jsonState(t, out).State != preview.StateStopped {
		t.Fatalf("status after stop: %s\n%s", errText, out)
	}
}

func TestPreviewCLIWithoutADeclaration(t *testing.T) {
	repo := t.TempDir()
	intent := filepath.Join(repo, "intent.yaml")
	if err := os.WriteFile(intent, []byte("apiVersion: sourceplane.io/v1\nkind: Intent\nmetadata:\n  name: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errText := runOrun(t, "preview", "start", "--intent", intent); !strings.Contains(errText, "no preview declared: intent.yaml has no preview.component") {
		t.Fatalf("start without a declaration: %q", errText)
	}
	out, errText := runOrun(t, "preview", "status", "--intent", intent)
	if errText != "" || !strings.Contains(out, "no preview has been started here") {
		t.Fatalf("status: %s\n%s", errText, out)
	}
}

func TestValidateReportsABadPreview(t *testing.T) {
	repo := previewRepo(t, 80)
	_, errText := runOrun(t, "validate", "--intent", filepath.Join(repo, "intent.yaml"))
	if !strings.Contains(errText, "preview validation failed:\n  component web: spec.preview.port must be an integer from 1024 to 65535") {
		t.Fatalf("validate: %q", errText)
	}
}

func TestPreviewCommandsReadNoCatalog(t *testing.T) {
	for _, args := range [][]string{{"preview", "status"}, {"preview", "logs"}, {"preview"}} {
		cmd, _, err := rootCmd.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		if commandUsesIntent(cmd) {
			t.Errorf("%v is treated as a catalog command (auto-refresh would run)", args)
		}
	}
	if cmd, _, _ := rootCmd.Find([]string{"status"}); !commandUsesIntent(cmd) {
		t.Error("orun status no longer uses the intent")
	}
}

// servePreview supervises a grounded coding session's checkout only, and its
// reports become preview_changed notices for the session log.
func TestServePreview(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	n := agent.NewNotices()
	var log bytes.Buffer
	if servePreview("", driver.ClaudeCodeID, env(nil), n, &log) != nil {
		t.Error("an ungrounded session runs a preview")
	}
	if servePreview("/repo", (&driver.Bootstrap{}).ID(), env(nil), n, &log) != nil {
		t.Error("a baseline build runs a preview")
	}
	if servePreview("/repo", driver.ClaudeCodeID, env(map[string]string{"ORUN_PREVIEW": "off"}), n, &log) != nil {
		t.Error("ORUN_PREVIEW=off runs a preview")
	}
	pv := servePreview("/repo", driver.ClaudeCodeID, env(nil), n, &log)
	if pv == nil || pv.Root != "/repo" {
		t.Fatalf("supervisor = %+v", pv)
	}
	pv.Emit(preview.Event{State: preview.StateReady, Port: 4173, Component: "web", Path: "/"})
	pv.Logf("starting %s", "web")

	// The notice is logged by a run loop; drive one with the stub driver.
	var got []agent.SessionEvent
	_, err := agent.Run(t.Context(), objectstore.NewMemStore(objectstore.AlgoSHA256), agent.RunOptions{
		SessionID: "as_preview", Driver: &driver.Stub{Script: []driver.Event{{Kind: driver.EventDone}}},
		Notices: n, Observe: func(e agent.SessionEvent) { got = append(got, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	for _, e := range got {
		if e.Kind == nodes.SessionEventPreviewChanged {
			payload = e.Payload
		}
	}
	if payload["state"] != "ready" || payload["port"] != 4173 || payload["component"] != "web" || payload["path"] != "/" {
		t.Fatalf("preview_changed payload = %v (events %v)", payload, got)
	}
	if !strings.Contains(log.String(), "orun agent serve: preview — starting web\n") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestRenderPreviewChangedLine(t *testing.T) {
	var out bytes.Buffer
	renderEventLine(&out, "preview_changed", map[string]any{"state": "ready", "component": "web", "port": float64(4173), "path": "/"})
	renderEventLine(&out, "preview_changed", map[string]any{"state": "failed", "component": "web", "port": 4173, "path": "/", "detail": "web exited"})
	want := "  [preview] ready web :4173/\n  [preview] failed web :4173/ — web exited\n"
	if out.String() != want {
		t.Fatalf("lines = %q, want %q", out.String(), want)
	}
}
