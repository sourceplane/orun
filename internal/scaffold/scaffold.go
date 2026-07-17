package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

// Options drives a scaffold/instantiate run (design §3 flow).
type Options struct {
	// Blueprint is the raw blueprint document bytes.
	Blueprint []byte
	// Inputs are the raw string-keyed input assignments (from flags/prompts).
	Inputs map[string]string
	// OutDir is the target directory (created if absent). Every placed file
	// must resolve inside it (design §9).
	OutDir string
	// Store pins sources and the blueprint by digest for provenance (design
	// §5/§11). Required.
	Store objectstore.ObjectStore
	// WorkDir is a scratch directory for git/oci materialization. If empty, a
	// temp dir is created and removed on return.
	WorkDir string
	// SourceBaseDir resolves relative `dir` source paths (e.g. a blueprint's
	// `path: .`). Set it to the blueprint's own directory so a scaffold works
	// from any CWD. Empty ⇒ relative paths resolve against the process CWD.
	SourceBaseDir string
	// RunHooks opts into executing declared hooks (design §12). Off by default:
	// hooks run outside the sandbox, so they are opt-in per instantiation.
	RunHooks bool
	// WorkflowEngine runs `workflow:` hooks (orun-workflows Surface B). Nil-safe:
	// when a workflow hook runs and this is nil, the engine is resolved from
	// ORUN_TORKFLOW_ENGINE. Tests inject a fake. Unused unless RunHooks is set.
	WorkflowEngine workflowbackend.Engine
}

// Result summarizes a completed scaffold.
type Result struct {
	// Order is the batched placement order (design §6).
	Order [][]string
	// Phases is the ordered phase plan (single implicit phase when the
	// blueprint declares none).
	Phases []PhasePlan
	// Files are the target-relative paths written.
	Files []string
	// Consumed are the recorded consume-mode dependencies (design §4).
	Consumed []ConsumedDep
	// Provenance is the lock written under .orun/provenance.lock (design §11).
	Provenance Provenance
	// HooksRun lists the hook ids executed (empty unless RunHooks).
	HooksRun []string
}

// Run executes the unified pipeline at whatever scale the blueprint implies
// (design §3): resolve blueprint → collect inputs → resolve sources → order
// modules → place each (template/copy/consume) → gate → write → provenance →
// hooks. It fails closed: any parse/containment/secret/gate/order failure is an
// error and no partial tree is presented as success.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("scaffold: object store is required for provenance")
	}
	bp, err := ParseBlueprint(opts.Blueprint)
	if err != nil {
		return nil, notFoundErr("%v", err)
	}
	values, err := CollectInputs(bp.Inputs, opts.Inputs)
	if err != nil {
		return nil, err
	}
	// Validate every workflow hook's connections grant BEFORE any placement
	// (orun-workflows-v2 §4): the grant must cover exactly the connections the
	// workflow file declares, and every granted input must be a declared
	// secret input. Fail-closed, with nothing written.
	if err := validateHookGrants(bp, opts.SourceBaseDir); err != nil {
		return nil, err
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "orun-scaffold-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(workDir)
	}

	sources, err := resolveSources(ctx, opts.Store, bp.Sources, bp.Ignore, opts.SourceBaseDir, workDir)
	if err != nil {
		return nil, err
	}

	phases, err := planPhases(bp)
	if err != nil {
		return nil, err
	}
	modulesByName := make(map[string]Module, len(bp.Modules))
	for _, m := range bp.Modules {
		modulesByName[m.Name] = m
	}

	// Place modules phase by phase, batch by batch, in dependency order. Detect
	// cross-module target collisions (S-10) — a silent last-writer is a failure.
	placed := map[string]PlacedFile{}
	var consumed []ConsumedDep
	var order [][]string
	for _, phase := range phases {
		for _, batch := range phase.Batches {
			order = append(order, batch)
			for _, name := range batch {
				m := modulesByName[name]
				var tree FileTree
				if m.Source != "" {
					rs, ok := sources[m.Source]
					if !ok {
						return nil, notFoundErr("module %q references unresolved source %q", m.Name, m.Source)
					}
					tree = rs.Tree
				}
				out, err := placeModule(m, tree, values)
				if err != nil {
					return nil, err
				}
				if out.consumed != nil {
					dep := *out.consumed
					if rs, ok := sources[dep.Source]; ok {
						dep.Digest = string(rs.Digest)
					}
					consumed = append(consumed, dep)
				}
				for _, f := range out.files {
					if prev, dup := placed[f.Path]; dup {
						return nil, gateErr("target collision at %q: modules %q and %q both write it (design §5/§9)", f.Path, prev.Module, f.Module)
					}
					placed[f.Path] = f
				}
			}
		}
	}

	// Output gate (design §10, component depth): every generated component.yaml
	// must pass both parsers before anything is written. Fail closed.
	for path, f := range placed {
		if isComponentYAML(path) {
			if err := gateComponentYAML(path, f.Bytes); err != nil {
				return nil, err
			}
		}
	}

	// Write the tree. Containment was enforced during placement; re-check at
	// write time against the real OutDir (symlink-out guard, design §9).
	if err := writeTree(opts.OutDir, placed); err != nil {
		return nil, err
	}

	// Provenance (design §11): blueprint@digest + source@digest(s) + inputs-hash
	// + per-module mode/target. Written even for a single scaffolded component.
	prov, err := buildProvenance(ctx, opts.Store, opts.Blueprint, bp, values, sources, placed, consumed, opts.SourceBaseDir)
	if err != nil {
		return nil, err
	}
	if err := writeProvenance(opts.OutDir, prov); err != nil {
		return nil, err
	}

	// Hooks (opt-in, outside the sandbox — design §12). Per-phase hooks run in
	// phase order, then the global postInstantiate hooks. Step-1 overlay: hooks
	// run after the tree is fully written (atomic); interleaved-per-phase writes
	// + approval gates are the planned resumable follow-on.
	var hooksRun []string
	if opts.RunHooks {
		hr := &hookRunner{
			outDir:  opts.OutDir,
			baseDir: opts.SourceBaseDir,
			engine:  opts.WorkflowEngine,
			secrets: values.SecretMap(),
			digests: hookDigestMap(prov),
		}
		for _, phase := range phases {
			ran, herr := hr.run(ctx, phase.Hooks)
			if herr != nil {
				return nil, herr
			}
			hooksRun = append(hooksRun, ran...)
		}
		ran, herr := hr.run(ctx, bp.Hooks.PostInstantiate)
		if herr != nil {
			return nil, herr
		}
		hooksRun = append(hooksRun, ran...)
	}

	files := make([]string, 0, len(placed))
	for p := range placed {
		files = append(files, p)
	}
	sort.Strings(files)

	return &Result{
		Order:      order,
		Phases:     phases,
		Files:      files,
		Consumed:   consumed,
		Provenance: prov,
		HooksRun:   hooksRun,
	}, nil
}

// writeTree flushes placed files to disk under outDir, creating parents. Each
// final path is re-verified to be inside outDir (design §9 fail-closed).
func writeTree(outDir string, placed map[string]PlacedFile) error {
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return err
	}
	paths := make([]string, 0, len(placed))
	for p := range placed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		target := filepath.Join(absOut, filepath.FromSlash(p))
		if !withinRoot(absOut, target) {
			return gateErr("write: %q escapes output root (design §9)", p)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, placed[p].Bytes, 0o644); err != nil {
			return err
		}
	}
	return nil
}
