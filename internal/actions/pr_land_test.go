package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/provenance"
)

// orun.pr/land@v1 against REAL git.
//
// The pen's own tests fake git with an answer of "main" to every question, and
// that is how an action that had never committed a byte shipped as the one that
// lands every phase — its first real run died on `rev-parse HEAD` in the
// repository `git init` had just made. So here only GitHub is fake. The product
// is a real repository, origin is a real bare one, and the fake GitHub merges a
// pull request by actually squashing it into that bare repository, so the pull
// that follows a landing is a real fast-forward or a real failure.
//
// The one git answer that is rewritten is `remote get-url origin`, and only for
// the pen: a bare directory is not on github.com, and the pen needs an
// owner/name to address the API.

const landOwner, landRepo = "acme", "product"

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// isolateGit keeps the developer's own git configuration out of the test —
// their init.defaultBranch, their hooks, their signing — and supplies an
// identity, so what is tested is what a fresh container does.
func isolateGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	for _, kv := range [][2]string{{"user.name", "dev"}, {"user.email", "dev@example.test"}, {"commit.gpgsign", "false"}} {
		if out, err := exec.Command("git", "config", "--global", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, out)
		}
	}
}

// fakeGitHub opens and squash-merges pull requests into a bare repository.
type fakeGitHub struct {
	t      *testing.T
	bare   string
	mu     sync.Mutex
	opened []map[string]any
	merged int
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := fmt.Sprintf("/repos/%s/%s/pulls", landOwner, landRepo)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == prefix:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// GitHub refuses a pull request between branches that share no
		// history, and so must this: a fake that merged anything into anything
		// passed a landing whose commit had no parent on main.
		head, base := "refs/heads/"+body["head"].(string), "refs/heads/"+body["base"].(string)
		if err := exec.Command("git", "-C", f.bare, "merge-base", head, base).Run(); err != nil {
			http.Error(w, `{"message":"Validation Failed","errors":[{"message":"`+body["base"].(string)+` and `+body["head"].(string)+` are entirely different commit histories."}]}`, http.StatusUnprocessableEntity)
			return
		}
		f.opened = append(f.opened, body)
		n := len(f.opened)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": n, "html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", landOwner, landRepo, n)})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, prefix+"/"):
		pr := f.pr(r.URL.Path, prefix)
		head := gitT(f.t, f.bare, "rev-parse", "refs/heads/"+pr["head"].(string))
		_ = json.NewEncoder(w).Encode(map[string]any{"head": map[string]any{"sha": head}})
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge"):
		pr := f.pr(strings.TrimSuffix(r.URL.Path, "/merge"), prefix)
		head, base := pr["head"].(string), pr["base"].(string)
		tree := gitT(f.t, f.bare, "rev-parse", "refs/heads/"+head+"^{tree}")
		parent := gitT(f.t, f.bare, "rev-parse", "refs/heads/"+base)
		sha := gitT(f.t, f.bare, "commit-tree", tree, "-p", parent, "-m", pr["title"].(string))
		gitT(f.t, f.bare, "update-ref", "refs/heads/"+base, sha)
		f.merged++
		_ = json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": sha})
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *fakeGitHub) pr(path, prefix string) map[string]any {
	var n int
	_, _ = fmt.Sscanf(strings.TrimPrefix(path, prefix+"/"), "%d", &n)
	if n < 1 || n > len(f.opened) {
		f.t.Fatalf("no pull request #%d", n)
	}
	return f.opened[n-1]
}

type landRig struct {
	product, bare string
	gh            *fakeGitHub
	pen           *provenance.Pen
}

// newLandRig is a product fresh from `git init` — on `master`, the stock
// default, so the base-branch rename is exercised — with an empty bare origin.
func newLandRig(t *testing.T) *landRig {
	t.Helper()
	isolateGit(t)
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	product := filepath.Join(root, "product")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", bare)
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, product, "init", "-q", "-b", "master")
	gitT(t, product, "remote", "add", "origin", bare)

	gh := &fakeGitHub{t: t, bare: bare}
	api := httptest.NewServer(gh)
	t.Cleanup(api.Close)
	real := gitDir(product)
	pen := &provenance.Pen{
		Workdir: product,
		Token:   func() string { return "tok" },
		APIBase: api.URL,
		HTTP:    api.Client(),
		RunGit: func(ctx context.Context, args ...string) (string, error) {
			if strings.Join(args, " ") == "remote get-url origin" {
				return fmt.Sprintf("git@github.com:%s/%s.git", landOwner, landRepo), nil
			}
			return real.run(ctx, args...)
		},
	}
	return &landRig{product: product, bare: bare, gh: gh, pen: pen}
}

func (r *landRig) write(t *testing.T, files map[string]string) {
	t.Helper()
	for path, body := range files {
		full := filepath.Join(r.product, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (r *landRig) land(t *testing.T, task, slug string) (Result, error) {
	t.Helper()
	params, err := Resolve("orun.pr/land@v1", map[string]any{
		"task": task, "branchSlug": slug, "title": "phase(" + slug + ")", "wait": false,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return landWith(context.Background(), r.pen, Input{Dir: r.product, Params: params})
}

// The first landing of every bootstrap: `git init`, files staged, no commit,
// nothing on the remote. It must end with the product on main, main on the
// remote, and the phase's files arriving as PR #1.
func TestTheFirstLandingIntoARepositoryWithNoHistory(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n", ".github/workflows/ci.yml": "on: push\n"})
	gitT(t, r.product, "add", "-A") // what the blueprint's git-stage hook does

	res, err := r.land(t, "BASE-1", "01-scaffold")
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if res.Outputs["landed"] != "true" || res.Outputs["merged"] != "true" {
		t.Errorf("outputs = %v, want landed and merged", res.Outputs)
	}
	if len(r.gh.opened) != 1 {
		t.Fatalf("want one pull request, got %d", len(r.gh.opened))
	}
	pr := r.gh.opened[0]
	if pr["base"] != "main" || pr["head"] != "orun/BASE-1-01-scaffold" {
		t.Errorf("PR #1 is %v → %v, want orun/BASE-1-01-scaffold → main", pr["head"], pr["base"])
	}
	// The seed is empty: everything the phase placed is in PR #1, not in a
	// commit nobody reviewed.
	seed := gitT(t, r.bare, "rev-list", "--max-parents=0", "main")
	if files := gitT(t, r.bare, "ls-tree", "-r", "--name-only", seed); files != "" {
		t.Errorf("the first commit should be empty, but holds %q", files)
	}
	if got := gitT(t, r.bare, "ls-tree", "-r", "--name-only", "main"); got != ".github/workflows/ci.yml\nREADME.md" {
		t.Errorf("main on the remote holds %q", got)
	}
	// Back on a pulled main — renamed from `master`, and not ahead of origin.
	if branch := gitT(t, r.product, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Errorf("the product is on %q, want main", branch)
	}
	if local, remote := gitT(t, r.product, "rev-parse", "HEAD"), gitT(t, r.bare, "rev-parse", "main"); local != remote {
		t.Errorf("local main %s is not origin's %s", local, remote)
	}
	if st := gitT(t, r.product, "status", "--porcelain"); st != "" {
		t.Errorf("the tree is not clean after landing:\n%s", st)
	}
	if b := gitT(t, r.product, "branch", "--list", "orun-landing/*"); b != "" {
		t.Errorf("the scratch branch was left behind: %s", b)
	}
}

// Every later phase: files WRITTEN, not staged, onto a product already on main.
func TestALaterPhaseLandsWhatItPlaced(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n"})
	if _, err := r.land(t, "BASE-1", "01-scaffold"); err != nil {
		t.Fatalf("first landing: %v", err)
	}
	r.write(t, map[string]string{"packages/db/index.ts": "export {}\n"})

	res, err := r.land(t, "BASE-2", "02-foundation")
	if err != nil {
		t.Fatalf("second landing: %v", err)
	}
	if res.Outputs["merged"] != "true" || len(r.gh.opened) != 2 {
		t.Fatalf("want PR #2 merged, got %v with %d PRs", res.Outputs, len(r.gh.opened))
	}
	if !strings.Contains(gitT(t, r.bare, "ls-tree", "-r", "--name-only", "main"), "packages/db/index.ts") {
		t.Error("the second phase's file did not reach main")
	}
	if local, remote := gitT(t, r.product, "rev-parse", "HEAD"), gitT(t, r.bare, "rev-parse", "main"); local != remote {
		t.Errorf("local main %s is not origin's %s after a squash merge", local, remote)
	}
}

// A phase that already landed lands nothing, successfully — not an empty PR.
func TestNothingToLandIsNotAnError(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n"})
	if _, err := r.land(t, "BASE-1", "01-scaffold"); err != nil {
		t.Fatalf("first landing: %v", err)
	}
	res, err := r.land(t, "BASE-1", "01-scaffold")
	if err != nil {
		t.Fatalf("re-landing: %v", err)
	}
	if res.Outputs["landed"] != "false" {
		t.Errorf("outputs = %v, want landed=false", res.Outputs)
	}
	if len(r.gh.opened) != 1 {
		t.Errorf("a re-run opened another pull request (%d total)", len(r.gh.opened))
	}
}

// A repository somebody pre-created with a licence must not have it deleted by
// the product's first landing.
func TestAPreCreatedRepositoryWithFilesIsNotLandedOver(t *testing.T) {
	r := newLandRig(t)
	other := t.TempDir()
	gitT(t, other, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(other, "LICENSE"), []byte("MIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, other, "add", "-A")
	gitT(t, other, "commit", "-q", "-m", "licence")
	gitT(t, other, "push", "-q", r.bare, "main")

	r.write(t, map[string]string{"README.md": "hi\n"})
	_, err := r.land(t, "BASE-1", "01-scaffold")
	if err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Fatalf("want a refusal naming LICENSE, got %v", err)
	}
	if len(r.gh.opened) != 0 {
		t.Error("a pull request was opened over the refusal")
	}
}

// A repository an earlier attempt already seeded is adopted, not seeded twice.
func TestAnAlreadySeededRemoteIsAdopted(t *testing.T) {
	r := newLandRig(t)
	r.write(t, map[string]string{"README.md": "hi\n"})
	gitT(t, r.product, "add", "-A")
	if _, err := seedBase(context.Background(), gitDir(r.product), "main", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seed := gitT(t, r.bare, "rev-parse", "main")

	// A fresh container: the same files, no local history, the seed upstream.
	fresh := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, fresh, "init", "-q", "-b", "master")
	gitT(t, fresh, "remote", "add", "origin", r.bare)
	if err := os.WriteFile(filepath.Join(fresh, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.product = fresh
	r.pen.Workdir = fresh
	real := gitDir(fresh)
	r.pen.RunGit = func(ctx context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") == "remote get-url origin" {
			return fmt.Sprintf("git@github.com:%s/%s.git", landOwner, landRepo), nil
		}
		return real.run(ctx, args...)
	}
	if _, err := r.land(t, "BASE-1", "01-scaffold"); err != nil {
		t.Fatalf("land: %v", err)
	}
	if roots := gitT(t, r.bare, "rev-list", "--max-parents=0", "main"); roots != seed {
		t.Errorf("main has root(s) %q, want only the original seed %s", roots, seed)
	}
}

// Nowhere to push is a message about the missing step, not a git error.
func TestLandingWithoutAnOriginNamesTheHookThatWiresIt(t *testing.T) {
	r := newLandRig(t)
	gitT(t, r.product, "remote", "remove", "origin")
	r.write(t, map[string]string{"README.md": "hi\n"})
	_, err := r.land(t, "BASE-1", "01-scaffold")
	if err == nil || !strings.Contains(err.Error(), "orun.repo/ensure@v1") {
		t.Fatalf("want an error naming orun.repo/ensure@v1, got %v", err)
	}
}

// orun.repo/ensure@v1 wires the product's origin — the half of its documented
// job it did not do.
func TestRepoEnsureWiresOriginOnlyWhereItShould(t *testing.T) {
	isolateGit(t)
	ctx := context.Background()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	product := t.TempDir()
	gitT(t, product, "init", "-q")
	url, err := wireOrigin(ctx, product, "sourceplane", "altocumulus")
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	if url != "git@github.com:sourceplane/altocumulus.git" || gitT(t, product, "remote", "get-url", "origin") != url {
		t.Errorf("without a token in the environment origin should be SSH, got %q", url)
	}

	// An existing origin is never retargeted.
	if again, _ := wireOrigin(ctx, product, "someone", "else"); again != url {
		t.Errorf("an existing origin was retargeted to %q", again)
	}

	// With a token in the environment, https.
	t.Setenv("GH_TOKEN", "tok")
	headless := t.TempDir()
	gitT(t, headless, "init", "-q")
	if url, _ := wireOrigin(ctx, headless, "sourceplane", "altocumulus"); url != "https://github.com/sourceplane/altocumulus.git" {
		t.Errorf("with a token in the environment origin should be https, got %q", url)
	}

	// A directory that is not itself a repository — even one inside another
	// checkout — is left alone.
	parent := t.TempDir()
	gitT(t, parent, "init", "-q")
	nested := filepath.Join(parent, "product")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if url, _ := wireOrigin(ctx, nested, "sourceplane", "altocumulus"); url != "" {
		t.Errorf("wired %q into a directory that is not a repository", url)
	}
	if out, _ := exec.Command("git", "-C", parent, "remote").CombinedOutput(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("the enclosing checkout's remotes were touched: %s", out)
	}
}
