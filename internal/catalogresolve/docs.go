package catalogresolve

// docs.go — doc-set resolution (saas-catalog-docs CD1). Resolves each entity's
// declared docs (the reserved `overview` + ordered `pages`) into
// catalogmodel.ResolvedDoc values: validated identity (key/title/role),
// repo-relative normalized paths, and bytes read at the pinned commit —
// refusing attachment (declared-only, logged reason) when the path is dirty or
// untracked so the recorded commit provenance can never lie (model.md §2d).
//
// Bounds (model.md §0): per-doc 256 KiB, ≤ 24 pages/entity (validate stage),
// 8 MiB doc budget per resolve. Over-cap docs stay declared-only with a
// reason; a doc problem never fails the resolve.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// docGitProbeTimeout bounds the two git invocations the probe runs; the
// resolve degrades to no-provenance attachment when git is slow/absent.
const docGitProbeTimeout = 5 * time.Second

// docGitState is the once-per-resolve git snapshot doc attachment consults:
// the HEAD commit and the set of paths whose working-tree state differs from
// it (modified, staged, or untracked). hasRepo=false means no usable git
// state — docs then attach with no commit recorded (honest: no provenance is
// claimed rather than a wrong one).
type docGitState struct {
	hasRepo bool
	commit  string
	dirty   map[string]bool
}

// probeDocGit resolves the git state for doc attachment. Best-effort: any
// failure yields hasRepo=false rather than an error — a doc problem never
// fails a resolve.
func probeDocGit(root string) *docGitState {
	ctx, cancel := context.WithTimeout(context.Background(), docGitProbeTimeout)
	defer cancel()
	run := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return out.String(), true
	}
	head, ok := run("rev-parse", "HEAD")
	if !ok {
		return &docGitState{}
	}
	status, ok := run("status", "--porcelain")
	if !ok {
		return &docGitState{}
	}
	dirty := map[string]bool{}
	for _, line := range strings.Split(status, "\n") {
		// Porcelain v1: two status chars, a space, then the path (arrows for
		// renames — record both sides).
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		for _, side := range strings.Split(p, " -> ") {
			side = strings.Trim(strings.TrimSpace(side), `"`)
			if side != "" {
				dirty[filepath.ToSlash(side)] = true
			}
		}
	}
	return &docGitState{hasRepo: true, commit: strings.TrimSpace(head), dirty: dirty}
}

// docBudget tracks the per-resolve closure byte budget (model.md §0).
type docBudget struct {
	remaining int
	exhausted bool
}

func newDocBudget() *docBudget { return &docBudget{remaining: catalogmodel.MaxDocClosureBytes} }

// take reserves n bytes; returns false once the budget is exhausted.
func (b *docBudget) take(n int) bool {
	if b.remaining < n {
		b.exhausted = true
		return false
	}
	b.remaining -= n
	return true
}

// normalizeDocPath resolves a declared doc path to a clean repo-relative
// forward-slash path: relative paths join baseDir (the declaring manifest's
// directory; "" for repo-root declarations like the `repo:` block). Returns
// ("", reason) when the path is absolute or escapes the repo.
func normalizeDocPath(baseDir, declared string) (string, string) {
	p := strings.TrimSpace(declared)
	if p == "" {
		return "", "empty path"
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(filepath.FromSlash(p)) {
		return "", "absolute paths are not allowed"
	}
	joined := path.Clean(path.Join(baseDir, filepath.ToSlash(p)))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", "path escapes the repository"
	}
	if joined == "." {
		return "", "not a file path"
	}
	return joined, ""
}

// validateDocPages runs the declared-shape checks for one entity's pages
// (model.md §2a): slug keys (reserved `overview`), slug roles, unique keys
// after filename-stem defaulting, and the page-count cap. Returned issues are
// SeverityError — a malformed declaration is an authoring bug, not a runtime
// condition. entityFile attributes the issue.
func validateDocPages(pages []catalogmodel.DocPage, entityFile string) []ValidationIssue {
	var issues []ValidationIssue
	add := func(code, msg string) {
		issues = append(issues, ValidationIssue{
			File: entityFile, Severity: SeverityError, Code: code, Message: msg,
		})
	}
	if len(pages) > catalogmodel.MaxDocPagesPerEntity {
		add("docs.pages.too-many", fmt.Sprintf("docs.pages declares %d pages; the maximum is %d", len(pages), catalogmodel.MaxDocPagesPerEntity))
	}
	seen := map[string]string{} // key → declaring path
	for i, pg := range pages {
		where := fmt.Sprintf("docs.pages[%d]", i)
		if strings.TrimSpace(pg.Path) == "" {
			add("docs.pages.path.missing", where+": path is required")
			continue
		}
		key := pg.Key
		if key == "" {
			key = catalogmodel.DocKeyFromPath(pg.Path)
		}
		switch {
		case key == "":
			add("docs.pages.key.underivable", fmt.Sprintf("%s (%s): no usable key derives from the filename; set `key` explicitly", where, pg.Path))
		case key == catalogmodel.DocKeyOverview:
			add("docs.pages.key.reserved", fmt.Sprintf("%s (%s): key %q is reserved for the front page — declare it as docs.overview", where, pg.Path, catalogmodel.DocKeyOverview))
		case !catalogmodel.IsDocSlug(key) || len(key) > catalogmodel.MaxDocKeyLen:
			add("docs.pages.key.invalid", fmt.Sprintf("%s (%s): key %q is not a slug ([a-z0-9][a-z0-9-]*, ≤ %d chars)", where, pg.Path, key, catalogmodel.MaxDocKeyLen))
		case seen[key] != "":
			add("docs.pages.key.duplicate", fmt.Sprintf("%s (%s): key %q collides with %s — name one explicitly", where, pg.Path, key, seen[key]))
		default:
			seen[key] = pg.Path
		}
		if pg.Role != "" && !catalogmodel.IsDocSlug(pg.Role) {
			add("docs.pages.role.invalid", fmt.Sprintf("%s (%s): role %q is not a slug", where, pg.Path, pg.Role))
		}
	}
	return issues
}

// resolveDocSet resolves one entity's declared docs into the ResolvedDoc set:
// the overview (key "overview", no role) first, then pages in declared order.
// baseDir is the declaring manifest's repo-relative directory ("" for the
// repo block). warn receives one line per doc that stays declared-only.
// Declared-shape validation (validateDocPages) is assumed to have run — a
// page that would have failed it is skipped here defensively.
func resolveDocSet(root, baseDir, overview string, pages []catalogmodel.DocPage, git *docGitState, budget *docBudget, warn func(string)) []catalogmodel.ResolvedDoc {
	if overview == "" && len(pages) == 0 {
		return nil
	}
	if git == nil {
		git = &docGitState{}
	}
	if budget == nil {
		budget = newDocBudget()
	}
	if warn == nil {
		warn = func(string) {}
	}

	var out []catalogmodel.ResolvedDoc
	resolveOne := func(declaredPath, key, title, role string) {
		d := catalogmodel.ResolvedDoc{Key: key, Title: title, Role: role}
		norm, reason := normalizeDocPath(baseDir, declaredPath)
		if reason != "" {
			d.Path = filepath.ToSlash(strings.TrimSpace(declaredPath))
			d.Reason = reason
			warn(fmt.Sprintf("docs: skipped %s (%s)", d.Path, reason))
			out = append(out, d)
			return
		}
		d.Path = norm

		// Pinned-commit gate (model.md §2d): with git state, a dirty/untracked
		// path refuses attachment — the recorded commit would describe bytes it
		// never contained. A clean tracked path's working-tree bytes ARE the
		// git object at HEAD, so the working-tree read is the fast path.
		if git.hasRepo && git.dirty[norm] {
			d.Reason = fmt.Sprintf("dirty or untracked at %s", shortCommit(git.commit))
			warn(fmt.Sprintf("docs: skipped %s (%s) — commit it to attach", norm, d.Reason))
			out = append(out, d)
			return
		}

		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(norm)))
		if err != nil {
			d.Reason = "unreadable: " + readReason(err)
			// Missing files are the common authoring state (declared ahead of
			// writing the doc) — visible on the entity, quiet in the log.
			out = append(out, d)
			return
		}
		if len(b) > catalogmodel.MaxDocBytes {
			d.Reason = fmt.Sprintf("%d bytes exceeds the %d KiB per-doc cap", len(b), catalogmodel.MaxDocBytes/1024)
			warn(fmt.Sprintf("docs: skipped %s (%s)", norm, d.Reason))
			out = append(out, d)
			return
		}
		if !budget.take(len(b)) {
			d.Reason = "per-resolve doc budget exhausted"
			warn(fmt.Sprintf("docs: skipped %s (%s — %d MiB cap)", norm, d.Reason, catalogmodel.MaxDocClosureBytes/(1024*1024)))
			out = append(out, d)
			return
		}

		d.Bytes = b
		sum := sha256.Sum256(b)
		d.SHA = hex.EncodeToString(sum[:])
		if git.hasRepo {
			d.Commit = git.commit
		}
		if d.Title == "" {
			d.Title = catalogmodel.DocTitleFromContent(b, norm)
		}
		out = append(out, d)
	}

	if overview != "" {
		resolveOne(overview, catalogmodel.DocKeyOverview, "", "")
	}
	seen := map[string]bool{catalogmodel.DocKeyOverview: true}
	for _, pg := range pages {
		if strings.TrimSpace(pg.Path) == "" {
			continue // validateDocPages already errored
		}
		key := pg.Key
		if key == "" {
			key = catalogmodel.DocKeyFromPath(pg.Path)
		}
		if key == "" || !catalogmodel.IsDocSlug(key) || len(key) > catalogmodel.MaxDocKeyLen || seen[key] {
			continue // validateDocPages already errored
		}
		seen[key] = true
		role := pg.Role
		if role == "" {
			role = catalogmodel.DocRoleDefault
		}
		resolveOne(pg.Path, key, pg.Title, role)
	}
	return out
}

// docsWarn is the skipped-doc log sink (never silent, never a failed plan —
// model.md §0). Package-level so tests can capture it.
var docsWarn = func(line string) { fmt.Fprintln(os.Stderr, "orun: "+line) }

// docResolveContext shares one git probe and one byte budget across every
// doc-set resolution of a single Resolve call. The probe is lazy: a workspace
// declaring no docs never shells out to git.
type docResolveContext struct {
	root   string
	git    *docGitState
	budget *docBudget
}

func newDocResolveContext(root string) *docResolveContext {
	return &docResolveContext{root: root}
}

func (c *docResolveContext) resolve(baseDir, overview string, pages []catalogmodel.DocPage) []catalogmodel.ResolvedDoc {
	if overview == "" && len(pages) == 0 {
		return nil
	}
	if c.git == nil {
		c.git = probeDocGit(c.root)
		c.budget = newDocBudget()
	}
	return resolveDocSet(c.root, baseDir, overview, pages, c.git, c.budget, docsWarn)
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	if c == "" {
		return "HEAD"
	}
	return c
}

func readReason(err error) string {
	if os.IsNotExist(err) {
		return "file not found"
	}
	return "read failed"
}
