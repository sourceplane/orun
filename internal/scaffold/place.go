package scaffold

import (
	"path"
	"sort"
	"strings"
)

// PlacedFile is one output file produced by placement, keyed later by its
// contained target-relative path.
type PlacedFile struct {
	// Path is the target-relative, slash-separated, path-contained location.
	Path string
	// Bytes is the final content (rendered or verbatim).
	Bytes []byte
	// Module is the module that produced it (for provenance + collision report).
	Module string
}

// ConsumedDep records a consume-mode dependency: a pinned prerequisite that
// emits no bytes but participates in ordering and provenance (design §4, §11).
type ConsumedDep struct {
	Module string
	Source string
	From   string
	Digest string
}

// moduleOutput is the result of placing a single module.
type moduleOutput struct {
	files    []PlacedFile
	consumed *ConsumedDep
}

// placeModule turns a module + validated inputs + a resolved source tree into
// placed output (design §4). tree may be nil for consume modules and for
// inline template modules (which carry their own bodies). It enforces path
// containment (§9), the bind lint (§4), and the secret rule (§8). It is pure:
// identical (module, inputs, tree) ⇒ identical output (§9).
func placeModule(m Module, tree FileTree, vals Values) (moduleOutput, error) {
	switch m.Mode {
	case ModeConsume:
		return moduleOutput{consumed: &ConsumedDep{
			Module: m.Name,
			Source: m.Source,
			From:   m.From,
		}}, nil
	case ModeTemplate:
		return placeTemplate(m, tree, vals)
	case ModeCopy:
		return placeCopy(m, tree, vals)
	default:
		return moduleOutput{}, gateErr("module %q: unknown mode %q", m.Name, m.Mode)
	}
}

func placeTemplate(m Module, tree FileTree, vals Values) (moduleOutput, error) {
	files, err := moduleFileList(m, tree)
	if err != nil {
		return moduleOutput{}, err
	}
	bind := bindSet(m.Bind)
	out := moduleOutput{}
	for _, srcRel := range files {
		body, err := readModuleFile(m, tree, srcRel)
		if err != nil {
			return moduleOutput{}, err
		}
		relToFrom := relativeToFrom(m, srcRel)

		// Bind lint (§4): a source-backed file outside bind that references
		// inputs is a lint error — keep the interpolation surface auditable.
		// Inline files are authored in the blueprint itself (fully auditable),
		// so they are implicitly bindable.
		inline := m.Source == "" && len(m.Files) > 0
		if !inline && !bind[relToFrom] && referencesInputs(string(body)) {
			return moduleOutput{}, gateErr("module %q: file %q interpolates inputs but is not in bind (design §4)", m.Name, relToFrom)
		}

		rendered, err := Render(m.Name+":"+srcRel, string(body), vals.Fields)
		if err != nil {
			return moduleOutput{}, err
		}
		if err := secretSweep(m.Name, relToFrom, rendered, vals.SecretValues()); err != nil {
			return moduleOutput{}, err
		}

		target, err := targetPath(m, srcRel, vals)
		if err != nil {
			return moduleOutput{}, err
		}
		out.files = append(out.files, PlacedFile{Path: target, Bytes: rendered, Module: m.Name})
	}
	sortPlaced(out.files)
	return out, nil
}

func placeCopy(m Module, tree FileTree, vals Values) (moduleOutput, error) {
	if tree == nil {
		return moduleOutput{}, gateErr("module %q: copy mode requires a source", m.Name)
	}
	files, err := moduleFileList(m, tree)
	if err != nil {
		return moduleOutput{}, err
	}
	out := moduleOutput{}
	for _, srcRel := range files {
		body, err := tree.ReadFile(srcRel)
		if err != nil {
			return moduleOutput{}, err
		}
		// copy is verbatim — no engine — but still passes the secret sweep (§8).
		if err := secretSweep(m.Name, srcRel, body, vals.SecretValues()); err != nil {
			return moduleOutput{}, err
		}
		target, err := targetPath(m, srcRel, vals)
		if err != nil {
			return moduleOutput{}, err
		}
		out.files = append(out.files, PlacedFile{Path: target, Bytes: body, Module: m.Name})
	}
	sortPlaced(out.files)
	return out, nil
}

// moduleFileList returns the source-relative file paths a module places, sorted.
func moduleFileList(m Module, tree FileTree) ([]string, error) {
	if m.Source == "" && len(m.Files) > 0 {
		names := make([]string, 0, len(m.Files))
		for k := range m.Files {
			names = append(names, k)
		}
		sort.Strings(names)
		return names, nil
	}
	if tree == nil {
		return nil, gateErr("module %q: no source tree and no inline files", m.Name)
	}
	return tree.List(m.From)
}

func readModuleFile(m Module, tree FileTree, srcRel string) ([]byte, error) {
	if m.Source == "" && len(m.Files) > 0 {
		return []byte(m.Files[srcRel]), nil
	}
	return tree.ReadFile(srcRel)
}

// relativeToFrom strips a module's From prefix from a source path, so bind
// entries can be written relative to the module root.
func relativeToFrom(m Module, srcRel string) string {
	if m.From == "" {
		return srcRel
	}
	from := path.Clean(m.From)
	if srcRel == from {
		return path.Base(srcRel)
	}
	if rel := strings.TrimPrefix(srcRel, from+"/"); rel != srcRel {
		return rel
	}
	return srcRel
}

// targetPath computes the contained target-relative path for a source file
// (design §9). For inline modules the key IS the (templated) target path. For
// source modules, the file's path under From is re-rooted under To. All path
// segments are rendered through the engine, then containment-checked.
func targetPath(m Module, srcRel string, vals Values) (string, error) {
	var raw string
	switch {
	case m.Source == "" && len(m.Files) > 0:
		raw = srcRel
	case srcRel == path.Clean(m.From) && m.From != "" && m.From != ".":
		// From names a single file: To is the exact target path (falling back
		// to From's own path when To is unset), not a directory to nest under.
		if m.To != "" {
			raw = m.To
		} else {
			raw = srcRel
		}
	default:
		to := m.To
		if to == "" {
			to = m.From
		}
		raw = path.Join(to, relativeToFrom(m, srcRel))
	}
	rendered, err := RenderPath(raw, vals.Fields)
	if err != nil {
		return "", err
	}
	return containedTarget(m.Name, rendered)
}

// containedTarget rejects any target that is absolute or escapes the output
// root after cleaning (design §9). Returns the clean, slash-separated path.
func containedTarget(module, p string) (string, error) {
	if p == "" {
		return "", gateErr("module %q: empty target path", module)
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", gateErr("module %q: target %q is absolute (design §9)", module, p)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return "", gateErr("module %q: target %q escapes output root (design §9)", module, p)
	}
	return clean, nil
}

// secretSweep fails if any collected secret value survives into written bytes
// (design §8). Binds template (interpolated secret) and copy (byte match).
func secretSweep(module, file string, data []byte, secrets []string) error {
	s := string(data)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if strings.Contains(s, secret) {
			return gateErr("module %q: file %q would contain a secret value — secrets are references, never literals (design §8)", module, file)
		}
	}
	return nil
}

func bindSet(bind []string) map[string]bool {
	set := make(map[string]bool, len(bind))
	for _, b := range bind {
		set[path.Clean(b)] = true
	}
	return set
}

func sortPlaced(files []PlacedFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}
