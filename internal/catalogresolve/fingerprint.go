package catalogresolve

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// ComponentFingerprint is one component's input fingerprint — the leaf-set of
// the change-detection virtual Merkle tree (orun-catalog-state/data-model.md
// §2b). It is presentation-neutral; objplan maps it onto nodes.ComponentFingerprint.
type ComponentFingerprint struct {
	ComponentKey string
	Dir          string            // workspace-relative component dir
	Subtree      string            // hash over the input file set ⊕ the global leaf
	Files        map[string]string // workspace-relative path → content hash
	GlobalDigest string            // hash of the catalog-relevant intent (shared leaf)
}

// fingerprintCandidates are the basenames in a component's input read-set: the
// manifest plus the inference candidates (mirrors infer.go). Plus the *.tf glob,
// handled separately. Over-approximating to the whole non-recursive dir is
// permitted by the spec but this bounded set keeps the resolve hot path cheap.
var fingerprintCandidates = map[string]struct{}{
	"component.yaml":     {},
	"component.yml":      {},
	"package.json":       {},
	"pnpm-lock.yaml":     {},
	"yarn.lock":          {},
	"package-lock.json":  {},
	"bun.lockb":          {},
	"Dockerfile":         {},
	"Containerfile":      {},
	"terraform.tf.json":  {},
	"Chart.yaml":         {},
	"README.md":          {},
}

// isFingerprintCandidate reports whether a basename is in the input read-set.
func isFingerprintCandidate(name string) bool {
	if _, ok := fingerprintCandidates[name]; ok {
		return true
	}
	return strings.HasSuffix(name, ".tf")
}

// computeFingerprints derives a fingerprint per manifest. root is the absolute
// workspace root; globalDigest is the shared intent leaf (computed once). The
// result is ordered by componentKey for determinism. Best-effort: an unreadable
// candidate file is skipped (a sound under-set is acceptable for a derived
// cache; the cockpit recomputes the same way).
func computeFingerprints(root string, manifests []*catalogmodel.ComponentManifest, globalDigest string) []ComponentFingerprint {
	out := make([]ComponentFingerprint, 0, len(manifests))
	for _, cm := range manifests {
		if cm == nil || cm.Identity.Path == "" {
			continue
		}
		dir := path.Dir(cm.Identity.Path)
		out = append(out, fingerprintForDir(root, dir, cm.Identity.ComponentKey, globalDigest))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ComponentKey < out[j].ComponentKey })
	return out
}

// fingerprintForDir lists the component dir non-recursively, hashes each
// candidate file's content, and folds the sorted leaf set ⊕ the global leaf into
// a single subtree hash.
func fingerprintForDir(root, dir, componentKey, globalDigest string) ComponentFingerprint {
	fp := ComponentFingerprint{
		ComponentKey: componentKey,
		Dir:          dir,
		GlobalDigest: globalDigest,
	}

	absDir := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(absDir)
	if err == nil {
		files := map[string]string{}
		for _, e := range entries {
			if e.IsDir() || !isFingerprintCandidate(e.Name()) {
				continue
			}
			data, rerr := os.ReadFile(filepath.Join(absDir, e.Name()))
			if rerr != nil {
				continue // best-effort: skip unreadable candidate
			}
			rel := joinSlash(dir, e.Name())
			files[rel] = "sha256:" + sha256Hex(data)
		}
		if len(files) > 0 {
			fp.Files = files
		}
	}

	fp.Subtree = "sha256:" + subtreeHash(fp.Files, globalDigest)
	return fp
}

// subtreeHash folds the file leaf set and the global leaf into one deterministic
// digest: sorted "path\x00hash\n" lines, then a "global\x00<digest>" line.
func subtreeHash(files map[string]string, globalDigest string) string {
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	h := sha256.New()
	for _, rel := range rels {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write([]byte(files[rel]))
		h.Write([]byte{'\n'})
	}
	h.Write([]byte("global"))
	h.Write([]byte{0})
	h.Write([]byte(globalDigest))
	return hex.EncodeToString(h.Sum(nil))
}

// computeGlobalDigest hashes the intent file bytes — a sound over-approximation
// of "the catalog-relevant intent blocks" (any intent change flips it). Returns
// "" when there is no intent file.
func computeGlobalDigest(intentAbs string) string {
	if intentAbs == "" {
		return ""
	}
	data, err := os.ReadFile(intentAbs)
	if err != nil {
		return ""
	}
	return "sha256:" + sha256Hex(data)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// joinSlash joins a slash dir and a basename, normalizing the workspace-root
// ("." dir) so files there are addressed bare (no leading "./").
func joinSlash(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}
