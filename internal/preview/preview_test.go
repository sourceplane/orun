package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// The test binary doubles as the dev server: a child started with
// ORUN_PREVIEW_TEST_SERVER set never runs the tests.
const helperEnv = "ORUN_PREVIEW_TEST_SERVER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "serve":
		serveForever()
	case "env":
		cwd, _ := os.Getwd()
		body := "CWD=" + cwd + "\n" + strings.Join(os.Environ(), "\n") + "\n"
		_ = os.WriteFile(os.Getenv("ORUN_PREVIEW_TEST_ENVFILE"), []byte(body), 0o644)
		serveForever()
	case "exit":
		fmt.Println("starting the dev server")
		fmt.Println("\x1b[31mboom: cannot find module 'vite'\x1b[0m")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(2)
}

// serveForever listens on PORT until it is killed.
func serveForever() {
	ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("PORT"))
	if err != nil {
		fmt.Println("listen:", err)
		os.Exit(1)
	}
	fmt.Println("listening on", ln.Addr())
	for {
		c, err := ln.Accept()
		if err != nil {
			os.Exit(0)
		}
		_ = c.Close()
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func component(name, preview string) string {
	return "apiVersion: sourceplane.io/v1\nkind: Component\nmetadata:\n  name: " + name +
		"\nspec:\n  type: app\n" + preview
}

const intentWithPreview = "apiVersion: sourceplane.io/v1\nkind: Intent\nmetadata:\n  name: p\ndiscovery:\n  roots: [apps]\npreview:\n  component: web\n"

func TestLoadOrdersNeedsFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "intent.yaml"), intentWithPreview)
	// web needs api and worker; worker needs api too: api starts once, first.
	writeFile(t, filepath.Join(root, "apps/web/component.yaml"), component("web",
		"  preview:\n    run: [pnpm, dev]\n    port: 4173\n    needs: [worker, api]\n    env: {MODE: dev}\n"))
	writeFile(t, filepath.Join(root, "apps/worker/component.yaml"), component("worker",
		"  preview:\n    run: [node, worker.js]\n    port: 8788\n    needs: [api]\n    path: /health\n"))
	writeFile(t, filepath.Join(root, "apps/api/component.yaml"), component("api",
		"  preview:\n    run: [node, api.js]\n    port: 8787\n"))
	writeFile(t, filepath.Join(root, "apps/docs/component.yaml"), component("docs",
		"  preview:\n    run: [node, docs.js]\n    port: 9000\n"))

	plan, err := Load(filepath.Join(root, "intent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range plan.Components {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "api,worker,web" {
		t.Fatalf("start order = %s, want api,worker,web", got)
	}
	web := plan.Target()
	if web.Name != "web" || web.Port != 4173 || web.Path != "/" || web.Env["MODE"] != "dev" {
		t.Fatalf("target = %+v", web)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(root, "apps/web"))
	if gotDir, _ := filepath.EvalSymlinks(web.Dir); gotDir != wantDir {
		t.Fatalf("web dir = %s, want %s", web.Dir, wantDir)
	}
	if plan.Components[1].Path != "/health" {
		t.Fatalf("worker path = %q", plan.Components[1].Path)
	}
}

func TestResolveRejectsBadDeclarations(t *testing.T) {
	pv := func(port int, needs ...string) *model.ComponentPreview {
		return &model.ComponentPreview{Run: []string{"x"}, Port: port, Needs: needs}
	}
	cases := []struct {
		name  string
		comps []model.Component
		want  string
	}{
		{"cycle", []model.Component{{Name: "web", Preview: pv(4173, "api")}, {Name: "api", Preview: pv(8787, "web")}}, "cycle: web → api → web"},
		{"missing need", []model.Component{{Name: "web", Preview: pv(4173, "ghost")}}, `needs names "ghost", which is no component`},
		{"need without preview", []model.Component{{Name: "web", Preview: pv(4173, "api")}, {Name: "api"}}, "needs names api, which declares no spec.preview"},
		{"no preview", []model.Component{{Name: "web"}}, "preview.component web declares no spec.preview"},
		{"bad port", []model.Component{{Name: "web", Preview: pv(80)}}, "spec.preview.port must be an integer from 1024 to 65535"},
		{"bad path", []model.Component{{Name: "web", Preview: &model.ComponentPreview{Run: []string{"x"}, Port: 4173, Path: "app"}}}, "path must start with /"},
		{"empty run", []model.Component{{Name: "web", Preview: &model.ComponentPreview{Port: 4173}}}, "run must be a non-empty argv list"},
		{"shared port", []model.Component{{Name: "web", Preview: pv(4173, "api")}, {Name: "api", Preview: pv(4173)}}, "both declare port 4173"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			intent := &model.Intent{Preview: &model.IntentPreview{Component: "web"}, Components: c.comps}
			_, err := Resolve(intent, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if errors.Is(err, ErrNotDeclared) {
				t.Fatalf("a bad declaration read as not declared: %v", err)
			}
			if errs := Validate(intent); !containsErr(errs, c.want) {
				t.Fatalf("Validate = %v, want %q", errs, c.want)
			}
		})
	}
}

// safeBuffer is a bytes.Buffer a follower writes while the test reads.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func containsErr(errs []error, want string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), want) {
			return true
		}
	}
	return false
}

func TestResolveNotDeclaredYet(t *testing.T) {
	pv := &model.ComponentPreview{Run: []string{"x"}, Port: 4173}
	for name, intent := range map[string]*model.Intent{
		"no preview":        {},
		"empty component":   {Preview: &model.IntentPreview{Component: " "}},
		"no such component": {Preview: &model.IntentPreview{Component: "web"}},
		"no directory":      {Preview: &model.IntentPreview{Component: "web"}, Components: []model.Component{{Name: "web", Path: "apps/web", Preview: pv}}},
	} {
		if _, err := Resolve(intent, t.TempDir()); !errors.Is(err, ErrNotDeclared) {
			t.Errorf("%s: err = %v, want ErrNotDeclared", name, err)
		}
	}
}

func TestValidate(t *testing.T) {
	good := &model.ComponentPreview{Run: []string{"x"}, Port: 4173}
	if errs := Validate(&model.Intent{}); len(errs) != 0 {
		t.Fatalf("no preview at all: %v", errs)
	}
	if errs := Validate(&model.Intent{Preview: &model.IntentPreview{Component: "web"}, Components: []model.Component{{Name: "web", Preview: good}}}); len(errs) != 0 {
		t.Fatalf("a good preview: %v", errs)
	}
	// Every declared spec.preview is checked, chosen or not; the intent's
	// choice must name a component that declares one.
	errs := Validate(&model.Intent{
		Preview:    &model.IntentPreview{Component: "ghost"},
		Components: []model.Component{{Name: "docs", Preview: &model.ComponentPreview{Port: 1}}},
	})
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	sort.Strings(msgs)
	want := []string{
		`component docs: spec.preview.port must be an integer from 1024 to 65535`,
		`component docs: spec.preview.run must be a non-empty argv list`,
		`preview.component "ghost" names no component`,
	}
	if strings.Join(msgs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Validate =\n%s\nwant\n%s", strings.Join(msgs, "\n"), strings.Join(want, "\n"))
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	if _, err := Detect(root); !errors.Is(err, ErrNotDeclared) {
		t.Fatalf("no intent: %v", err)
	}
	writeFile(t, filepath.Join(root, "intent.yaml"), "apiVersion: sourceplane.io/v1\nkind: Intent\nmetadata:\n  name: p\n")
	if _, err := Detect(root); !errors.Is(err, ErrNotDeclared) {
		t.Fatalf("no preview: %v", err)
	}
	// The intent names the preview before its code has landed.
	writeFile(t, filepath.Join(root, "intent.yaml"), intentWithPreview)
	if _, err := Detect(root); !errors.Is(err, ErrNotDeclared) {
		t.Fatalf("no apps/ yet: %v", err)
	}
	writeFile(t, filepath.Join(root, "apps/api/component.yaml"), component("api", ""))
	if _, err := Detect(root); !errors.Is(err, ErrNotDeclared) {
		t.Fatalf("no web yet: %v", err)
	}
	writeFile(t, filepath.Join(root, "apps/web/component.yaml"), component("web", "  preview:\n    run: [pnpm, dev]\n    port: 99\n"))
	if _, err := Detect(root); err == nil || errors.Is(err, ErrNotDeclared) {
		t.Fatalf("bad port: %v", err)
	}
	writeFile(t, filepath.Join(root, "apps/web/component.yaml"), component("web", "  preview:\n    run: [pnpm, dev]\n    port: 4173\n"))
	plan, err := Detect(root)
	if err != nil || plan.Target().Name != "web" {
		t.Fatalf("declared: %+v, %v", plan, err)
	}
}

func TestFingerprintFollowsTheDeclaration(t *testing.T) {
	a := &Plan{Components: []Component{{Name: "web", Run: []string{"vite"}, Port: 4173, Path: "/"}}}
	b := &Plan{Components: []Component{{Name: "web", Run: []string{"vite"}, Port: 4174, Path: "/"}}}
	if a.Fingerprint() == b.Fingerprint() || a.Fingerprint() != (&Plan{Components: a.Components}).Fingerprint() {
		t.Fatal("fingerprint does not follow the declaration")
	}
}

func TestChildEnvStripsSecrets(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin", "HOME=/home/box", "NODE_ENV=development", "ORUN_SESSION_ID=as_1",
		"ORUN_SESSION_TOKEN=s3cret", "ORUN_TOKEN=t", "ORUN_TOKEN_FILE=/run/token",
		"ANTHROPIC_API_KEY=k", "ANTHROPIC_BASE_URL=https://gw", "OPENROUTER_API_KEY=k",
		"GH_TOKEN=g", "GH_HOST=github.com", "GITHUB_TOKEN=g", "CLOUDFLARE_API_TOKEN=c",
		"AWS_SECRET_ACCESS_KEY=a", "DB_PASSWORD=p", "STRIPE_SECRET=s", "my_api_key=lower",
		"PORT=8080", "MODE=prod",
	}
	got := ChildEnv(environ, map[string]string{"MODE": "dev", "VITE_PUBLIC_KEY": "pk"}, 4173)
	want := []string{"PATH=/usr/bin", "HOME=/home/box", "NODE_ENV=development", "ORUN_SESSION_ID=as_1",
		"PORT=4173", "MODE=dev", "VITE_PUBLIC_KEY=pk"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("ChildEnv =\n%v\nwant\n%v", got, want)
	}
	// spec.preview.env may set PORT itself.
	if got := ChildEnv([]string{"PORT=1"}, map[string]string{"PORT": "5000"}, 4173); strings.Join(got, " ") != "PORT=5000" {
		t.Fatalf("own PORT = %v", got)
	}
}

func TestCopyLog(t *testing.T) {
	root := t.TempDir()
	if err := CopyLog(context.Background(), &bytes.Buffer{}, root, "web", false); err == nil {
		t.Fatal("no log: want an error")
	}
	writeFile(t, LogPath(root, "web"), "one\n")
	var out bytes.Buffer
	if err := CopyLog(context.Background(), &out, root, "web", false); err != nil || out.String() != "one\n" {
		t.Fatalf("copy = %q, %v", out.String(), err)
	}

	// Follow picks up what is appended.
	ctx, cancel := context.WithCancel(context.Background())
	var followed safeBuffer
	done := make(chan error, 1)
	go func() { done <- CopyLog(ctx, &followed, root, "web", true) }()
	f, err := os.OpenFile(LogPath(root, "web"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, "two")
	f.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(followed.String(), "two") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if followed.String() != "one\ntwo\n" {
		t.Fatalf("followed = %q", followed.String())
	}
}

func TestLastLogLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, LogPath(root, "web"), "orun preview: web — vite\nready\n\x1b[31merror: port taken\x1b[0m\r\n\n")
	if got := lastLogLine(LogPath(root, "web")); got != "error: port taken" {
		t.Fatalf("lastLogLine = %q", got)
	}
	if LogPath(root, "a/b c") != filepath.Join(Dir(root), "a_b_c.log") {
		t.Fatalf("LogPath does not sanitize: %s", LogPath(root, "a/b c"))
	}
}

func TestEventPayload(t *testing.T) {
	p := Event{State: StateReady, Port: 4173, Component: "web", Path: "/"}.Payload()
	if len(p) != 4 || p["state"] != "ready" || p["port"] != 4173 || p["component"] != "web" || p["path"] != "/" {
		t.Fatalf("payload = %v", p)
	}
	if p := (Event{State: StateFailed, Detail: "boom"}).Payload(); len(p) != 2 || p["detail"] != "boom" {
		t.Fatalf("failed payload = %v", p)
	}
}
