package sourcectx

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// DirtyFile is one entry in the dirty-hash input set per identity-and-keys.md
// §7. Path is the repo-relative POSIX path; Content is the raw file bytes.
//
// The C1 dirty-file probe materializes []DirtyFile from the workspace; C0
// only fixes the type and the deterministic hash function.
type DirtyFile struct {
	Path    string
	Content []byte
}

// DirtyHash returns the canonical `sha256:<hex>` over a list of dirty files
// per identity-and-keys.md §7:
//
//   - Sort entries lexicographically by Path.
//   - For each entry: write "<path>\0<sha256-of-content>\n".
//   - Hash the concatenation with SHA-256.
//
// Pure and deterministic. Returns ("sha256:<empty-tree-hash>", nil) for an
// empty list — callers that want "no dirty hash at all" should check len(in)
// before calling.
func DirtyHash(files []DirtyFile) string {
	sorted := make([]DirtyFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	outer := sha256.New()
	for _, f := range sorted {
		inner := sha256.Sum256(f.Content)
		outer.Write([]byte(f.Path))
		outer.Write([]byte{0})
		outer.Write([]byte(hex.EncodeToString(inner[:])))
		outer.Write([]byte{'\n'})
	}
	sum := outer.Sum(nil)
	return "sha256:" + hex.EncodeToString(sum)
}

// CatalogInputHashInputs is the §8 ordered input bundle used to derive
// catalogInputHash. Field order in this struct is irrelevant — the hash is
// computed by writing fields in the documented canonical order, not via
// struct reflection.
type CatalogInputHashInputs struct {
	TreeHash        string
	DirtyHash       string
	OrunVersion     string
	ResolverVersion int
	SchemaVersion   string
	StackSources    []string
	// IntentCanonical is the pre-canonicalized JSON of the
	// `intent.yaml.catalog` block (with empty defaults inlined). Caller is
	// responsible for running this through catalogmodel.CanonicalEncode.
	IntentCanonical []byte
}

// CatalogInputHash returns the canonical `sha256:<hex>` over the §8 input
// bundle. Inputs are joined with a single `\n` and hashed once.
//
// Pure and deterministic.
func CatalogInputHash(in CatalogInputHashInputs) string {
	stacks := make([]string, len(in.StackSources))
	copy(stacks, in.StackSources)
	sort.Strings(stacks)

	var b strings.Builder
	b.WriteString(in.TreeHash)
	b.WriteByte('\n')
	b.WriteString(in.DirtyHash)
	b.WriteByte('\n')
	b.WriteString(in.OrunVersion)
	b.WriteByte('\n')
	b.WriteString(itoa(in.ResolverVersion))
	b.WriteByte('\n')
	b.WriteString(in.SchemaVersion)
	b.WriteByte('\n')
	for _, s := range stacks {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	b.Write(in.IntentCanonical)

	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
