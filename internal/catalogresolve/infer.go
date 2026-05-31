package catalogresolve

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// inferenceConfig mirrors the resolved intent.catalog.inference block
// after defaulting. Every flag is concrete (no pointer ambiguity) so
// downstream stage 6 can branch on raw booleans.
type inferenceConfig struct {
	Enabled     bool
	PackageJSON bool
	Dockerfile  bool
	Terraform   bool
	Helm        bool
	Readme      bool
}

// resolveInferenceConfig produces the runtime config from the parsed
// intent.yaml. Master switch defaults to TRUE; per-flag toggles
// inherit the master switch unless the user pinned them explicitly.
func resolveInferenceConfig(in *intentInference) inferenceConfig {
	cfg := inferenceConfig{Enabled: true, PackageJSON: true, Dockerfile: true, Terraform: true, Helm: true, Readme: true}
	if in == nil {
		return cfg
	}
	if in.Enabled != nil {
		cfg.Enabled = *in.Enabled
	}
	apply := func(dst *bool, override *bool) {
		if !cfg.Enabled {
			*dst = false
			return
		}
		if override != nil {
			*dst = *override
		}
	}
	apply(&cfg.PackageJSON, in.PackageJSON)
	apply(&cfg.Dockerfile, in.Dockerfile)
	apply(&cfg.Terraform, in.Terraform)
	apply(&cfg.Helm, in.Helm)
	apply(&cfg.Readme, in.Readme)
	return cfg
}

// frameworkRule is one entry in the fixed-order framework heuristic
// list (resolution-pipeline.md §7). The first matching rule wins on a
// tie; ordering is deliberate and load-bearing for determinism.
type frameworkRule struct {
	pkgKey    string // dependency name to look up in package.json deps
	framework string // emitted into runtime.inferred.frameworks
}

// frameworkRules is the canonical fixed-order list (§4 + §7). Adding
// a rule MUST append; reordering changes inferred outputs.
var frameworkRules = []frameworkRule{
	{"next", "next"},
	{"vite", "vite"},
	{"hono", "hono"},
	{"express", "express"},
	{"fastify", "fastify"},
	{"react", "react"},
	{"vue", "vue"},
	{"svelte", "svelte"},
	{"@sveltejs/kit", "sveltekit"},
}

// infer runs stage 6 (resolution-pipeline.md §4) for a single
// authored manifest. The resolved manifest is mutated in-place; new
// resolution.inferredFrom entries are returned via cm.Resolution.
//
// Inference is additive: explicit runtime.inferred.* lists in the
// resolved manifest are preserved and inference results are unioned in.
//
// Errors are wrapped as *ErrInferenceFailed and surfaced via the
// `issues` channel; the caller (default mode) logs and skips. Strict
// mode promotes them to errors at the validate stage.
func infer(workspaceRoot string, am AuthoredManifest, cm *catalogmodel.ComponentManifest, cfg inferenceConfig) ([]*ErrInferenceFailed, []ValidationIssue) {
	if !cfg.Enabled {
		return nil, nil
	}

	// componentDir is the workspace-relative directory the manifest
	// was authored in (e.g. "apps/api-edge"). Inference scans only
	// this directory non-recursively for the canonical files
	// resolution-pipeline.md §4 lists.
	componentDir := pathDir(am.SourceFile)
	absDir := filepath.Join(workspaceRoot, filepath.FromSlash(componentDir))

	if cm.Resolution.InferredFrom == nil {
		cm.Resolution.InferredFrom = map[string][]string{}
	}

	var (
		failures []*ErrInferenceFailed
		issues   []ValidationIssue
	)

	addInferred := func(field, value, fromFile string) {
		switch field {
		case "languages":
			cm.Runtime.Inferred.Languages = appendUnique(cm.Runtime.Inferred.Languages, value)
		case "packageManagers":
			cm.Runtime.Inferred.PackageManagers = appendUnique(cm.Runtime.Inferred.PackageManagers, value)
		case "frameworks":
			cm.Runtime.Inferred.Frameworks = appendUnique(cm.Runtime.Inferred.Frameworks, value)
		case "infra":
			cm.Runtime.Inferred.Infra = appendUnique(cm.Runtime.Inferred.Infra, value)
		}
		key := "runtime.inferred." + field + "." + value
		cm.Resolution.InferredFrom[key] = appendUnique(cm.Resolution.InferredFrom[key], fromFile)
	}

	// 1. package.json + lockfiles
	if cfg.PackageJSON {
		pkgPath := filepath.Join(absDir, "package.json")
		if _, statErr := os.Stat(pkgPath); statErr == nil {
			rel := filepath.ToSlash(filepath.Join(componentDir, "package.json"))
			deps, err := readPackageJSON(pkgPath)
			if err != nil {
				failures = append(failures, &ErrInferenceFailed{Path: rel, Reason: "package.json parse", Underlying: err})
			} else {
				addInferred("languages", "javascript", rel)
				if hasTypescriptDep(deps) {
					addInferred("languages", "typescript", rel)
				}
				for _, rule := range frameworkRules {
					if _, present := deps[rule.pkgKey]; present {
						addInferred("frameworks", rule.framework, rel)
					}
				}
				cm.Runtime.Files.Package = strPtr(rel)
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			failures = append(failures, &ErrInferenceFailed{Path: pathJoin(componentDir, "package.json"), Reason: "stat", Underlying: statErr})
		}

		// lockfiles → packageManagers
		for _, lf := range []struct{ name, mgr string }{
			{"pnpm-lock.yaml", "pnpm"},
			{"yarn.lock", "yarn"},
			{"package-lock.json", "npm"},
			{"bun.lockb", "bun"},
		} {
			lp := filepath.Join(absDir, lf.name)
			if _, err := os.Stat(lp); err == nil {
				addInferred("packageManagers", lf.mgr, pathJoin(componentDir, lf.name))
			}
		}
	}

	// 2. Dockerfile
	if cfg.Dockerfile {
		for _, name := range []string{"Dockerfile", "Containerfile"} {
			fp := filepath.Join(absDir, name)
			if _, err := os.Stat(fp); err == nil {
				rel := pathJoin(componentDir, name)
				addInferred("infra", "docker", rel)
				cm.Runtime.Files.Dockerfile = strPtr(rel)
				break
			}
		}
	}

	// 3. Terraform
	if cfg.Terraform {
		entries, err := os.ReadDir(absDir)
		if err == nil {
			tfNames := []string{}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if strings.HasSuffix(name, ".tf") || name == "terraform.tf.json" {
					tfNames = append(tfNames, name)
				}
			}
			sort.Strings(tfNames)
			for _, n := range tfNames {
				addInferred("infra", "terraform", pathJoin(componentDir, n))
				break // one provenance entry is enough; uniqueness deduped
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, &ErrInferenceFailed{Path: componentDir, Reason: "readdir", Underlying: err})
		}
	}

	// 4. Helm
	if cfg.Helm {
		fp := filepath.Join(absDir, "Chart.yaml")
		if _, err := os.Stat(fp); err == nil {
			addInferred("infra", "helm", pathJoin(componentDir, "Chart.yaml"))
		}
	}

	// 5. README — back-fill metadata.description if unset
	if cfg.Readme {
		fp := filepath.Join(absDir, "README.md")
		if _, err := os.Stat(fp); err == nil {
			rel := pathJoin(componentDir, "README.md")
			cm.Runtime.Files.Readme = strPtr(rel)
			if cm.Metadata.Description == "" {
				para, readErr := readFirstParagraph(fp)
				if readErr != nil {
					failures = append(failures, &ErrInferenceFailed{Path: rel, Reason: "readme read", Underlying: readErr})
				} else if para != "" {
					cm.Metadata.Description = para
					if cm.Resolution.InheritedFrom == nil {
						cm.Resolution.InheritedFrom = map[string]string{}
					}
					cm.Resolution.InheritedFrom["metadata.description"] = rel
				}
			}
		}
	}

	// Deterministic ordering: sort every inferred list and every
	// inferredFrom value list.
	sort.Strings(cm.Runtime.Inferred.Languages)
	sort.Strings(cm.Runtime.Inferred.PackageManagers)
	sort.Strings(cm.Runtime.Inferred.Frameworks)
	sort.Strings(cm.Runtime.Inferred.Infra)
	for k := range cm.Resolution.InferredFrom {
		sort.Strings(cm.Resolution.InferredFrom[k])
	}

	return failures, issues
}

// readPackageJSON returns a string-keyed merged dependency map
// (dependencies + devDependencies + peerDependencies). Values are
// dropped — only the key set matters for framework heuristics.
func readPackageJSON(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pj struct {
		Dependencies     map[string]string `json:"dependencies"`
		DevDependencies  map[string]string `json:"devDependencies"`
		PeerDependencies map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(raw, &pj); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(pj.Dependencies)+len(pj.DevDependencies)+len(pj.PeerDependencies))
	for _, m := range []map[string]string{pj.Dependencies, pj.DevDependencies, pj.PeerDependencies} {
		for k, v := range m {
			out[k] = v
		}
	}
	return out, nil
}

func hasTypescriptDep(deps map[string]string) bool {
	for _, k := range []string{"typescript", "@types/node", "ts-node", "tsx"} {
		if _, present := deps[k]; present {
			return true
		}
	}
	return false
}

// readFirstParagraph returns the first non-empty markdown paragraph
// from path. Trims leading "#"-headings and ATX setext underlines so
// the description is prose rather than the page title.
func readFirstParagraph(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(raw), "\n")
	var para []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		// Skip heading lines outright.
		if strings.HasPrefix(trim, "#") {
			continue
		}
		// Skip setext underlines (=== / ---).
		if strings.IndexFunc(trim, func(r rune) bool { return r != '=' && r != '-' }) == -1 {
			continue
		}
		para = append(para, trim)
	}
	return strings.Join(para, " "), nil
}

// appendUnique appends v to s only if s does not already contain it.
// Used so explicit author-set values survive inference unioning.
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func strPtr(s string) *string { return &s }

// pathDir is filepath.Dir but always slash-separated. Returns "" for
// a leaf component.yaml authored at the workspace root.
func pathDir(slashPath string) string {
	idx := strings.LastIndex(slashPath, "/")
	if idx < 0 {
		return ""
	}
	return slashPath[:idx]
}

func pathJoin(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out == "" {
			out = p
			continue
		}
		out = out + "/" + p
	}
	return out
}
