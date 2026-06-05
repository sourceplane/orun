package affected

import (
	"context"
	"strings"

	"github.com/sourceplane/orun/internal/git"
)

// GitChangeSource is the ChangeSource backed by a git diff — the migration of
// today's --changed path. It wraps git.ChangeDetector for the file set and reads
// the base/head intent.yaml bytes (for the semantic intent diff) the same way
// the legacy cmd/orun path did.
type GitChangeSource struct {
	Options    git.ChangeOptions
	IntentPath string // workspace-relative intent path; defaults to "intent.yaml"
}

// ChangedPaths returns the changed file set and the intent signal. When intent
// changed but its before/after bytes can't be read (the --files mode, or a
// missing git ref), the bytes are left nil and the engine treats the change as
// conservatively global (CD-1).
func (g GitChangeSource) ChangedPaths(ctx context.Context) ([]string, IntentChange, error) {
	files, err := git.NewChangeDetectorWithOptions(g.Options).GetChangedFiles()
	if err != nil {
		return nil, IntentChange{}, err
	}

	intentPath := g.IntentPath
	if intentPath == "" {
		intentPath = "intent.yaml"
	}
	normIntent := normPath(intentPath)

	ic := IntentChange{}
	if intentInFiles(files, normIntent) {
		ic.Changed = true
		// A semantic diff needs the file at two refs; impossible under --files.
		if len(g.Options.Files) == 0 {
			base := g.Options.Base
			if base == "" {
				base = "main"
			}
			head := g.Options.Head
			if head == "" {
				head = "HEAD"
			}
			if b, e := git.GetFileAtRef(base, normIntent); e == nil {
				ic.Base = b
			}
			if h, e := git.GetFileAtRef(head, normIntent); e == nil {
				ic.Head = h
			}
		}
	}
	return files, ic, nil
}

// intentInFiles reports whether the (normalized) intent path is among the
// changed files, matching the legacy isIntentPathChanged: an exact match or a
// basename match (handles relative/absolute discovery paths).
func intentInFiles(files []string, normIntent string) bool {
	if normIntent == "" {
		return false
	}
	base := normIntent
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	for _, f := range files {
		nf := normPath(f)
		if nf == normIntent || nf == base || strings.HasSuffix(nf, "/"+base) {
			return true
		}
	}
	return false
}

func normPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.TrimPrefix(p, "./")
}
