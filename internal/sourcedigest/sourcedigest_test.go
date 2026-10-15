package sourcedigest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "dist/\nnode_modules/\n")
	write(t, filepath.Join(root, "apps/web/src/index.ts"), "export {}\n")
	write(t, filepath.Join(root, "apps/web/package.json"), `{"name":"web"}`)
	write(t, filepath.Join(root, "apps/api/src/main.go"), "package main\n")
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

func digest(t *testing.T, root, rel string) string {
	t.Helper()
	d, err := Digest(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != len("sha256:")+64 {
		t.Fatalf("malformed digest %q", d)
	}
	return d
}

func TestDigest_InAGitCheckoutMovesWithTheWorkTree(t *testing.T) {
	root := gitRepo(t)
	web := digest(t, root, "apps/web")
	if web != digest(t, root, "apps/web") {
		t.Fatal("the digest is deterministic")
	}
	api := digest(t, root, "apps/api")
	if web == api {
		t.Fatal("two directories have two digests")
	}

	// An edit elsewhere leaves the job's digest alone.
	write(t, filepath.Join(root, "apps/api/src/main.go"), "package main // edited\n")
	if digest(t, root, "apps/web") != web {
		t.Fatal("a change outside the job's directory does not move its digest")
	}
	if digest(t, root, "apps/api") == api {
		t.Fatal("an unstaged edit moves the digest")
	}

	// An untracked file counts; an ignored one does not.
	write(t, filepath.Join(root, "apps/web/src/new.ts"), "export const n = 1\n")
	withNew := digest(t, root, "apps/web")
	if withNew == web {
		t.Fatal("an untracked file moves the digest")
	}
	write(t, filepath.Join(root, "apps/web/dist/index.js"), "built\n")
	write(t, filepath.Join(root, "apps/web/node_modules/x/index.js"), "dep\n")
	if digest(t, root, "apps/web") != withNew {
		t.Fatal("ignored build outputs and dependencies do not move the digest")
	}

	// A deletion counts.
	if err := os.Remove(filepath.Join(root, "apps/web/package.json")); err != nil {
		t.Fatal(err)
	}
	if digest(t, root, "apps/web") == withNew {
		t.Fatal("a deleted tracked file moves the digest")
	}

	// The whole tree, and an absolute path inside it.
	whole := digest(t, root, "")
	if abs := digest(t, root, filepath.Join(root, "apps", "web")); abs != digest(t, root, "apps/web") {
		t.Fatalf("an absolute path inside the root is the same directory: %s vs %s", abs, digest(t, root, "apps/web"))
	}
	if whole == digest(t, root, "apps/web") {
		t.Fatal("the whole tree is not one directory")
	}
	if _, err := Digest(root, "apps/missing"); err == nil {
		t.Fatal("a missing directory is an error, not a digest")
	}
	if _, err := Digest(root, filepath.Join(t.TempDir(), "elsewhere")); err == nil {
		t.Fatal("a path outside the root is an error")
	}
}

func TestDigest_OutsideGitHashesTheFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "svc/main.py"), "print(1)\n")
	write(t, filepath.Join(root, "svc/node_modules/dep/index.js"), "dep\n")
	write(t, filepath.Join(root, "svc/dist/out.js"), "built\n")
	first := digest(t, root, "svc")
	if first != digest(t, root, "svc") {
		t.Fatal("deterministic")
	}
	write(t, filepath.Join(root, "svc/node_modules/dep/index.js"), "dep changed\n")
	write(t, filepath.Join(root, "svc/dist/out.js"), "rebuilt\n")
	if digest(t, root, "svc") != first {
		t.Fatal("dependencies and build outputs are not source")
	}
	write(t, filepath.Join(root, "svc/main.py"), "print(2)\n")
	if digest(t, root, "svc") == first {
		t.Fatal("an edit moves the digest")
	}
	if err := os.Chmod(filepath.Join(root, "svc/main.py"), 0o755); err != nil {
		t.Fatal(err)
	}
	second := digest(t, root, "svc")
	write(t, filepath.Join(root, "svc/main.py"), "print(2)\n")
	if err := os.Chmod(filepath.Join(root, "svc/main.py"), 0o644); err != nil {
		t.Fatal(err)
	}
	if digest(t, root, "svc") == second {
		t.Fatal("a mode change moves the digest")
	}
}
