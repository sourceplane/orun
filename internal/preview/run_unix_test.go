//go:build unix

package preview

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// freePort is a port nothing listens on, from the kernel.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// helperComponent runs this test binary as a dev server in mode.
func helperComponent(t *testing.T, root, name, mode string, port int, env map[string]string) Component {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "apps", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e := map[string]string{helperEnv: mode}
	for k, v := range env {
		e[k] = v
	}
	return Component{Name: name, Dir: dir, Run: []string{exe}, Port: port, Path: "/", Env: e}
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatalf("pid %d still alive", pid)
	}
}

func TestStartStatusStop(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "web.env")
	api := helperComponent(t, root, "api", "serve", freePort(t), nil)
	web := helperComponent(t, root, "web", "env", freePort(t), map[string]string{
		"ORUN_PREVIEW_TEST_ENVFILE": envFile, "MODE": "preview",
	})
	plan := &Plan{Root: root, Components: []Component{api, web}}
	t.Cleanup(func() { _, _ = Stop(root) })
	environ := append(os.Environ(),
		"ORUN_SESSION_TOKEN=s3cret", "ANTHROPIC_API_KEY=sk-ant", "GH_TOKEN=gho", "GITHUB_TOKEN=ghs",
		"OPENROUTER_API_KEY=or", "STRIPE_SECRET=sk", "PLAIN_VALUE=kept")

	st, err := Start(context.Background(), plan, Options{Timeout: 20 * time.Second, Environ: environ})
	if err != nil {
		t.Fatalf("start: %v (%+v)", err, st)
	}
	if st.State != StateReady || st.Component != "web" || st.Port != web.Port || st.Path != "/" || st.StartedAt == "" {
		t.Fatalf("state = %+v", st)
	}
	if len(st.Processes) != 2 || st.Processes[0].Component != "api" || st.Processes[1].Component != "web" {
		t.Fatalf("processes = %+v (needs first)", st.Processes)
	}
	onDisk, err := ReadState(root)
	if err != nil || onDisk.State != StateReady || onDisk.Processes[1].PID != st.Processes[1].PID {
		t.Fatalf("state.json = %+v, %v", onDisk, err)
	}
	// The state and logs never reach a commit.
	if ignore, err := os.ReadFile(filepath.Join(Dir(root), ".gitignore")); err != nil || !strings.HasSuffix(string(ignore), "\n*\n") {
		t.Fatalf(".orun/preview/.gitignore = %q, %v", ignore, err)
	}

	// The server ran from its directory, with its env, and without secrets.
	body, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	env := string(body)
	wantDir, _ := filepath.EvalSymlinks(web.Dir)
	for _, want := range []string{"CWD=" + wantDir + "\n", "MODE=preview\n", "PLAIN_VALUE=kept\n", "PORT=" + strconv.Itoa(web.Port) + "\n"} {
		if !strings.Contains(env, want) {
			t.Errorf("server env lacks %q", strings.TrimSpace(want))
		}
	}
	for _, secret := range []string{"s3cret", "sk-ant", "gho", "ghs", "OPENROUTER_API_KEY", "STRIPE_SECRET"} {
		if strings.Contains(env, secret) {
			t.Errorf("server env leaks %q", secret)
		}
	}

	// Idempotent: a healthy preview is left alone.
	again, err := Start(context.Background(), plan, Options{Timeout: 20 * time.Second, Environ: environ})
	if err != nil || again.Processes[0].PID != st.Processes[0].PID || again.Processes[1].PID != st.Processes[1].PID {
		t.Fatalf("second start restarted it: %+v, %v", again, err)
	}
	if cur, err := Status(root); err != nil || cur.State != StateReady {
		t.Fatalf("status = %+v, %v", cur, err)
	}

	// The server dies: status says so, and start brings it back.
	webPID := st.Processes[1].PID
	_ = syscall.Kill(-webPID, syscall.SIGKILL)
	waitDead(t, webPID)
	cur, err := Status(root)
	if err != nil || cur.State != StateFailed || !strings.Contains(cur.Detail, "web exited") {
		t.Fatalf("status after a crash = %+v, %v", cur, err)
	}
	if onDisk, _ := ReadState(root); onDisk.State != StateFailed {
		t.Fatalf("status did not record the crash: %+v", onDisk)
	}
	restarted, err := Start(context.Background(), plan, Options{Timeout: 20 * time.Second, Environ: environ})
	if err != nil || restarted.State != StateReady || restarted.Processes[1].PID == webPID {
		t.Fatalf("restart = %+v, %v", restarted, err)
	}
	waitDead(t, st.Processes[0].PID) // the old api was stopped, not leaked

	stopped, err := Stop(root)
	if err != nil || stopped.State != StateStopped {
		t.Fatalf("stop = %+v, %v", stopped, err)
	}
	for _, p := range restarted.Processes {
		waitDead(t, p.PID)
		if portAnswers(p.Port) {
			t.Errorf("%s still answers on %d", p.Component, p.Port)
		}
	}
	if cur, err := Status(root); err != nil || cur.State != StateStopped {
		t.Fatalf("status after stop = %+v, %v", cur, err)
	}
}

func TestStartFailsWhenTheServerExits(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{Root: root, Components: []Component{helperComponent(t, root, "web", "exit", freePort(t), nil)}}
	t.Cleanup(func() { _, _ = Stop(root) })
	st, err := Start(context.Background(), plan, Options{Timeout: 20 * time.Second})
	if err == nil || st.State != StateFailed {
		t.Fatalf("start = %+v, %v; want failed", st, err)
	}
	want := "web exited (exit status 3): boom: cannot find module 'vite'"
	if st.Detail != want {
		t.Fatalf("detail = %q, want %q", st.Detail, want)
	}
	if onDisk, _ := ReadState(root); onDisk.State != StateFailed || onDisk.Detail != want {
		t.Fatalf("state.json = %+v", onDisk)
	}
}

func TestStartTimesOutAndStopsTheServer(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{Root: root, Components: []Component{helperComponent(t, root, "web", "hang", freePort(t), nil)}}
	t.Cleanup(func() { _, _ = Stop(root) })
	st, err := Start(context.Background(), plan, Options{Timeout: 700 * time.Millisecond})
	if err == nil || st.State != StateFailed || !strings.Contains(st.Detail, "did not answer") {
		t.Fatalf("start = %+v, %v", st, err)
	}
	if len(st.Processes) != 1 {
		t.Fatalf("processes = %+v", st.Processes)
	}
	waitDead(t, st.Processes[0].PID)
}

func TestStartRefusesAPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	root := t.TempDir()
	port := ln.Addr().(*net.TCPAddr).Port
	plan := &Plan{Root: root, Components: []Component{helperComponent(t, root, "web", "serve", port, nil)}}
	st, err := Start(context.Background(), plan, Options{Timeout: 5 * time.Second})
	if err == nil || st.State != StateFailed || !strings.Contains(st.Detail, "already in use") || len(st.Processes) != 0 {
		t.Fatalf("start = %+v, %v", st, err)
	}
}

// The supervisor on a real checkout: it waits for the declaration, starts the
// server, restarts it when it dies, and stops it with the session.
func TestSupervisorOnARealCheckout(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	r := &recorder{}
	s := NewSupervisor(root, r.emit)
	s.CheckEvery, s.DetectEvery, s.Backoff, s.Timeout = 20*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond, 20*time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() { _, _ = Stop(root) })
	go s.Run(ctx)

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for r.states() != want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := r.states(); got != want {
			t.Fatalf("events = %q, want %q", got, want)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if r.states() != "" {
		t.Fatalf("events before any code: %q", r.states())
	}
	// The code lands.
	writeFile(t, filepath.Join(root, "intent.yaml"), intentWithPreview)
	writeFile(t, filepath.Join(root, "apps/web/component.yaml"), component("web",
		"  preview:\n    run: ["+strconv.Quote(exe)+"]\n    port: "+strconv.Itoa(port)+"\n    env: {"+helperEnv+": serve}\n"))
	p := ":" + strconv.Itoa(port)
	waitFor("starting" + p + ",ready" + p)

	st, err := ReadState(root)
	if err != nil {
		t.Fatal(err)
	}
	pid := st.Processes[0].PID
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	waitFor("starting" + p + ",ready" + p + ",starting" + p + ",ready" + p)

	st, _ = ReadState(root)
	cancel()
	s.Wait(20 * time.Second)
	waitFor("starting" + p + ",ready" + p + ",starting" + p + ",ready" + p + ",stopped" + p)
	waitDead(t, st.Processes[0].PID)
}
