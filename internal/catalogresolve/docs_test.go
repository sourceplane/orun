package catalogresolve

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestValidateDocPages(t *testing.T) {
	cases := []struct {
		name  string
		pages []catalogmodel.DocPage
		codes []string
	}{
		{"valid", []catalogmodel.DocPage{{Path: "docs/a.md"}, {Path: "docs/b.md", Key: "second", Role: "runbook"}}, nil},
		{"missing path", []catalogmodel.DocPage{{Key: "x"}}, []string{"docs.pages.path.missing"}},
		{"reserved overview", []catalogmodel.DocPage{{Path: "docs/overview.md"}}, []string{"docs.pages.key.reserved"}},
		{"bad key", []catalogmodel.DocPage{{Path: "docs/a.md", Key: "Bad_Key"}}, []string{"docs.pages.key.invalid"}},
		{"bad role", []catalogmodel.DocPage{{Path: "docs/a.md", Role: "Not A Slug"}}, []string{"docs.pages.role.invalid"}},
		{"duplicate stems", []catalogmodel.DocPage{{Path: "docs/setup.md"}, {Path: "ops/setup.md"}}, []string{"docs.pages.key.duplicate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := validateDocPages(tc.pages, "apps/api/component.yaml")
			var got []string
			for _, i := range issues {
				got = append(got, i.Code)
				if i.Severity != SeverityError {
					t.Errorf("%s: severity = %v, want error", i.Code, i.Severity)
				}
				if i.File != "apps/api/component.yaml" {
					t.Errorf("%s: file = %q", i.Code, i.File)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.codes, ",") {
				t.Errorf("codes = %v, want %v", got, tc.codes)
			}
		})
	}

	// Page-count cap.
	many := make([]catalogmodel.DocPage, catalogmodel.MaxDocPagesPerEntity+1)
	for i := range many {
		many[i] = catalogmodel.DocPage{Path: "docs/p.md", Key: "k" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))}
	}
	issues := validateDocPages(many, "intent.yaml")
	found := false
	for _, i := range issues {
		if i.Code == "docs.pages.too-many" {
			found = true
		}
	}
	if !found {
		t.Error("expected docs.pages.too-many")
	}
}

func TestNormalizeDocPath(t *testing.T) {
	for _, tc := range []struct{ base, in, want, reason string }{
		{"", "docs/a.md", "docs/a.md", ""},
		{"apps/api", "docs/a.md", "apps/api/docs/a.md", ""},
		{"apps/api", "./docs/a.md", "apps/api/docs/a.md", ""},
		{"apps/api", "../shared/a.md", "apps/shared/a.md", ""},
		{"", "../outside.md", "", "path escapes the repository"},
		{"", "/abs.md", "", "absolute paths are not allowed"},
		{"", "  ", "", "empty path"},
	} {
		got, reason := normalizeDocPath(tc.base, tc.in)
		if got != tc.want || reason != tc.reason {
			t.Errorf("normalizeDocPath(%q, %q) = (%q, %q), want (%q, %q)", tc.base, tc.in, got, reason, tc.want, tc.reason)
		}
	}
}

// gitDo runs git in dir, failing the test on error.
func gitDo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out.String())
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDocSetPinnedCommitGate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	write(t, dir, "docs/clean.md", "# Clean\n\nbody\n")
	write(t, dir, "docs/dirty.md", "# Dirty\n\ncommitted\n")
	gitDo(t, dir, "init", "-q")
	gitDo(t, dir, "add", ".")
	gitDo(t, dir, "commit", "-q", "-m", "init")
	// Dirty one tracked file + one untracked file after the commit.
	write(t, dir, "docs/dirty.md", "# Dirty\n\nedited but uncommitted\n")
	write(t, dir, "docs/untracked.md", "# Untracked\n")

	var warned []string
	oldWarn := docsWarn
	docsWarn = func(l string) { warned = append(warned, l) }
	defer func() { docsWarn = oldWarn }()

	ctx := newDocResolveContext(dir)
	docs := ctx.resolve("", "docs/clean.md", []catalogmodel.DocPage{
		{Path: "docs/dirty.md"},
		{Path: "docs/untracked.md"},
	})
	if len(docs) != 3 {
		t.Fatalf("docs = %d, want 3", len(docs))
	}
	clean, dirty, untracked := docs[0], docs[1], docs[2]

	if !clean.Attached() || clean.Commit == "" {
		t.Errorf("clean doc should attach with commit provenance: %+v", clean)
	}
	if dirty.Attached() || !strings.Contains(dirty.Reason, "dirty or untracked") {
		t.Errorf("dirty doc must refuse attachment: %+v", dirty)
	}
	if untracked.Attached() {
		t.Errorf("untracked doc must refuse attachment: %+v", untracked)
	}
	if len(warned) < 2 {
		t.Errorf("skips must be logged, got %v", warned)
	}
}

func TestResolveDocSetNoGitAttachesWithoutCommit(t *testing.T) {
	dir := t.TempDir() // no git repo
	write(t, dir, "docs/a.md", "# A\n")
	docs := newDocResolveContext(dir).resolve("", "docs/a.md", nil)
	if len(docs) != 1 || !docs[0].Attached() {
		t.Fatalf("docs = %+v", docs)
	}
	if docs[0].Commit != "" {
		t.Errorf("no-git attachment must not claim a commit, got %q", docs[0].Commit)
	}
}

func TestResolveDocSetCaps(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docs/big.md", strings.Repeat("x", catalogmodel.MaxDocBytes+1))
	write(t, dir, "docs/ok.md", "# OK\n")

	var warned []string
	oldWarn := docsWarn
	docsWarn = func(l string) { warned = append(warned, l) }
	defer func() { docsWarn = oldWarn }()

	docs := newDocResolveContext(dir).resolve("", "", []catalogmodel.DocPage{
		{Path: "docs/big.md"},
		{Path: "docs/ok.md"},
	})
	if docs[0].Attached() || !strings.Contains(docs[0].Reason, "per-doc cap") {
		t.Errorf("over-cap doc = %+v", docs[0])
	}
	if !docs[1].Attached() {
		t.Errorf("in-cap doc should still attach: %+v", docs[1])
	}
	if len(warned) != 1 {
		t.Errorf("warned = %v", warned)
	}

	// Budget exhaustion: shrink the budget and confirm the cutoff is logged.
	b := &docBudget{remaining: 2}
	got := resolveDocSet(dir, "", "", []catalogmodel.DocPage{{Path: "docs/ok.md"}}, &docGitState{}, b, func(string) {})
	if got[0].Attached() || !strings.Contains(got[0].Reason, "budget") {
		t.Errorf("budget-exhausted doc = %+v", got[0])
	}
}

func TestResolveDocSetDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "apps/api/docs/on-call-runbook.md", "# On-call\n")
	write(t, dir, "apps/api/docs/no-title.md", "plain text, no heading\n")
	docs := newDocResolveContext(dir).resolve("apps/api", "", []catalogmodel.DocPage{
		{Path: "docs/on-call-runbook.md", Role: "runbook"},
		{Path: "docs/no-title.md"},
	})
	if docs[0].Key != "on-call-runbook" {
		t.Errorf("key = %q, want filename stem", docs[0].Key)
	}
	if docs[0].Path != "apps/api/docs/on-call-runbook.md" {
		t.Errorf("path = %q, want repo-relative", docs[0].Path)
	}
	if docs[0].Title != "On-call" {
		t.Errorf("title = %q, want H1", docs[0].Title)
	}
	if docs[1].Title != "No Title" {
		t.Errorf("title = %q, want filename title-case", docs[1].Title)
	}
	if docs[1].Role != catalogmodel.DocRoleDefault {
		t.Errorf("role = %q, want default", docs[1].Role)
	}
}
