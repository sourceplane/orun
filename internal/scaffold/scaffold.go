package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
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
	// Workflow hooks run through the in-process flow engine (orun-workflows-v3
	// WA5) — there is no external engine to configure.
	RunHooks bool

	// Actions runs `uses:` hooks. Nil selects the real registry
	// (orun-bootstrap-engine BE-O1); a test or a dry-run simulator substitutes
	// a recording runner here.
	Actions ActionRunner

	// Events receives the build event stream (orun-bootstrap-engine BE-O6).
	// Nil means nobody is listening, which is the default and costs nothing.
	Events EventSink
	// RunID names this attempt in the stream. Empty means one is derived.
	RunID string

	// Phase selection (orun-bootstrap-engine BE-O2). Empty/false = every
	// phase, which is today's behavior.
	//
	// Only names one phase to place. Until places every phase through the
	// named one. Resume places every phase Derive does not already report as
	// done. At most one may be set.
	Only   string
	Until  string
	Resume bool
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
	plan, err := buildPlanWith(ctx, opts, bp, values)
	if err != nil {
		return nil, err
	}
	if plan.cleanup != nil {
		defer plan.cleanup()
	}
	phases, sources, placed, consumed, order := plan.phases, plan.sources, plan.placed, plan.consumed, plan.order

	// Phase selection (BE-O2): narrow what is WRITTEN, never what is COMPUTED.
	// The whole blueprint is always rendered, because collision detection and
	// the output gate are only meaningful against the complete set — a phase
	// that collides with one it was not asked to place is still a collision.
	selected, narrowed, err := plan.selectPhases(opts)
	if err != nil {
		return nil, err
	}
	partial := narrowed && len(selected) != len(plan.phases)
	if narrowed {
		phases = selected
		placed = plan.filesFor(selected)
	}

	// Drop phases the blueprint's own condition excludes (BE-O3). A declared
	// condition beats a caller remembering not to ask for the phase.
	kept := make([]PhasePlan, 0, len(phases))
	for _, ph := range phases {
		skip, serr := plan.skipped(ph.Name, values.Fields)
		if serr != nil {
			return nil, serr
		}
		if !skip {
			kept = append(kept, ph)
		}
	}
	if len(kept) != len(phases) {
		phases = kept
		placed = plan.filesFor(kept)
		partial = true
	}

	// Preconditions, before anything is written. `requires.phases` derives the
	// tree; `requires.probe` asks reality.
	//
	// `placing` accumulates in phase order, so when phase N is checked it holds
	// exactly the phases this run writes before N. A requirement satisfied by
	// this run is satisfied (BE-O11).
	placing := make(map[string]bool, len(phases))
	// A probe that answers "not yet" up here is DEFERRED, not fatal. Its
	// subject may be something a phase this very run is about to do — phase 04
	// asks for the bindings phase 03 publishes — so the only honest time to
	// ask is immediately before the phase runs. Recorded here, re-asked there.
	// A probe that fails outright still fails now, before a byte is written.
	deferredProbes := make(map[string]bool, len(phases))
	for _, ph := range phases {
		if err := checkRequires(ctx, plan, opts, ph, opts.Actions, placing); err != nil {
			if _, waiting := actions.IsPending(err); !waiting {
				return nil, err
			}
			deferredProbes[ph.Name] = true
		}
		placing[ph.Name] = true
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

	// Provenance (design §11): blueprint@digest + source@digest(s) + inputs-hash
	// + per-module mode/target. Written even for a single scaffolded component,
	// and re-written as each phase lands on disk, so the lock never names a
	// file that is not there.
	//
	// A partial run must not erase the record of the phases it did not touch:
	// `--phase 05-edge` writing a lock that names only five files would lose
	// everything phases 01-04 placed. The previous lock is read ONCE, before
	// this run writes anything, so a lock re-written between phases is never
	// mistaken for the one the run started from.
	var prevProv *Provenance
	if partial {
		if prev, rerr := ReadProvenance(opts.OutDir); rerr == nil {
			prevProv = &prev
		}
	}
	written := make(map[string]PlacedFile, len(placed))
	var prov Provenance
	// place writes files — containment was enforced during placement; writeTree
	// re-checks against the real OutDir (symlink-out guard, design §9) — and
	// records everything written so far.
	place := func(files map[string]PlacedFile) error {
		if err := writeTree(opts.OutDir, files); err != nil {
			return err
		}
		for path, f := range files {
			written[path] = f
		}
		p, err := buildProvenance(ctx, opts.Store, opts.Blueprint, bp, values, sources, written, consumed)
		if err != nil {
			return err
		}
		if prevProv != nil {
			p.Modules = mergeModuleRecords(prevProv.Modules, p.Modules)
		}
		if err := writeProvenance(opts.OutDir, p); err != nil {
			return err
		}
		prov = p
		return nil
	}

	// WHEN THE TREE IS WRITTEN depends on whether anything runs between the
	// phases.
	//
	// Without hooks, nothing does, so the whole tree is written at once — the
	// gates above have already passed against the complete set.
	//
	// With hooks, each phase's files are written between its `pre` and `post`
	// hooks, which is what those slots have always been documented to mean
	// ("pre: before placement", "post: after placement"). Writing the whole
	// tree up front meant a phase's post hooks ran over every LATER phase's
	// files too, and a bootstrap's phase 01 landed the entire product:
	//
	//	orun baseline new cirrus --local --run-hooks
	//	→ 01-scaffold stages 1075 files — every worker, the console, terraform
	//
	// before 03-infrastructure had minted a single credential those files
	// deploy with. Phases exist so each one lands, converges and is verified
	// before the next is on disk; that is only true if the next is not on disk.
	if !opts.RunHooks || len(phases) == 0 {
		if err := place(placed); err != nil {
			return nil, err
		}
	}

	// Hooks (opt-in, outside the sandbox — design §12). Per-phase hooks run in
	// phase order, each phase placed between its pre and post hooks, then the
	// global postInstantiate hooks.
	em := newEmitter(opts.Events, runIDOf(opts))
	// Every phase the blueprint declares but this run is not placing is
	// reported once, so a feed shows the whole shape rather than only the part
	// that moved.
	//
	// WHY it is not placed is part of the line. A resumed build leaves the
	// phases an earlier run placed, and reporting those as "not needed for
	// this build" — the words for a phase whose condition excludes it — told
	// the operator of a resumed console build that its repository's first two
	// phases were unnecessary ("The repo is born is not needed for this
	// build").
	for _, ph := range plan.phases {
		if containsPhase(phases, ph.Name) {
			continue
		}
		decl := plan.declOf(ph.Name)
		narration := renderNarration("", phaseTitle(decl, ph.Name), EventSkipped, nil)
		if excluded, _ := plan.skipped(ph.Name, values.Fields); !excluded {
			if opts.Resume {
				narration = fmt.Sprintf("%s is already in place from an earlier run.", ph.Name)
			} else {
				narration = fmt.Sprintf("%s is not part of this run.", ph.Name)
			}
		}
		em.emit(ctx, Event{Phase: ph.Name, State: EventSkipped, Narration: narration})
	}

	var hooksRun []string
	if opts.RunHooks {
		runner := opts.Actions
		if runner == nil {
			runner = DefaultActionRunner()
		}
		hr := &hookRunner{
			outDir:  opts.OutDir,
			baseDir: opts.SourceBaseDir,
			actions: runner,
			inputs:  values.nonSecretFields(),
		}
		for i, phase := range phases {
			decl := plan.declOf(phase.Name)
			title := phaseTitle(decl, phase.Name)
			// What `{{ .phase.name }}` resolves to for this phase's hooks.
			hr.phase = Phase{}
			if decl != nil {
				hr.phase = *decl
			}
			started := time.Now()
			// What an authored line may reference (BE-O10). `narrate` rebuilds
			// it as the phase progresses, because the facts a caption wants —
			// elapsed, the hook outputs — are not known when the phase starts.
			startMeta := phaseMeta(decl, plan.byPhase[phase.Name])
			narrate := func(state EventState, meta map[string]string) string {
				return renderNarration(decl.NarrateLine(state), title, state,
					narrationScope(phase.Name, title, hr.inputs, meta, hr.outputs))
			}
			em.emit(ctx, Event{Phase: phase.Name, State: EventStarted,
				Narration: narrate(EventStarted, startMeta),
				Meta:      startMeta})
			// A waiting event names what it waits on, in the detail and — as
			// `.meta.waitingOn` — in the line, so the headline says it too.
			waitingFor := func(step, reason string) Event {
				meta := withWaitingOn(startMeta, reason)
				return Event{Phase: phase.Name, Step: step, State: EventWaiting,
					Narration: narrate(EventWaiting, meta), Detail: reason, Meta: meta}
			}
			// The deferred probe, asked at the only moment its answer means
			// anything: the phases before it have now run. Still pending parks
			// the run exactly as a pending hook does, so `--resume` picks it up
			// when the thing it waits for exists.
			//
			// SAY SO, THEN WAIT. Asked once first: if the answer is not yet, the
			// build reports what it is waiting on BEFORE it spends the probe's
			// `waitSeconds` on it, so the operator who has to fix it is told
			// while there is still time to — not ten silent minutes later.
			if deferredProbes[phase.Name] {
				perr := checkProbes(ctx, plan, opts, phase, runner, false)
				if pe, waiting := actions.IsPending(perr); waiting {
					em.emit(ctx, waitingFor(pe.ID, pe.Reason))
					perr = checkProbes(ctx, plan, opts, phase, runner, true)
					if perr == nil {
						// Came good while waiting. Said, too: the page drew this
						// phase as waiting on somebody, and nothing else the
						// phase emits arrives until its hooks have finished.
						em.emit(ctx, Event{Phase: phase.Name, State: EventRunning,
							Narration: narrate(EventRunning, startMeta), Meta: startMeta})
					}
				}
				if perr != nil {
					if pe, waiting := actions.IsPending(perr); waiting {
						parked := &ParkedError{Phase: phase.Name, Hook: pe.ID, Reason: pe.Reason, RetryAfter: pe.RetryAfter}
						em.emit(ctx, waitingFor(pe.ID, pe.Reason))
						writeRunState(opts.OutDir, parked)
						return nil, parked
					}
					return nil, perr
				}
			}
			// pre, placement and post are retried together per the phase's
			// declared policy; outputs are scoped to the phase and reset on
			// every attempt.
			phaseFiles := plan.filesFor([]PhasePlan{phase})
			ran, herr := runPhaseHooks(ctx, hr, decl, phase.Hooks.Pre,
				func() error { return place(phaseFiles) }, phase.Hooks.Post)
			hooksRun = append(hooksRun, ran...)
			if pe, waiting := actions.IsPending(herr); waiting {
				parked := &ParkedError{Phase: phase.Name, Hook: pe.ID, Reason: pe.Reason, RetryAfter: pe.RetryAfter}
				em.emit(ctx, waitingFor(pe.ID, pe.Reason))
				writeRunState(opts.OutDir, parked)
				return nil, parked
			}
			if herr != nil {
				em.emit(ctx, Event{Phase: phase.Name, State: EventFailed,
					Narration: narrate(EventFailed, startMeta),
					Detail:    herr.Error()})
				return nil, fmt.Errorf("phase %q: %w", phase.Name, herr)
			}
			emitHookNarrations(ctx, em, phase.Name,
				narrationScope(phase.Name, title, hr.inputs, startMeta, hr.outputs),
				phase.Hooks.Pre, phase.Hooks.Post)
			// await runs OUTSIDE the retry policy. Retrying a wait would turn
			// "still running" into an error after N attempts, when the honest
			// answer is that it is still running.
			awaited, aerr := runAwait(ctx, hr, phase.Name, phase.Hooks.Await)
			hooksRun = append(hooksRun, awaited...)
			if parked, ok := aerr.(*ParkedError); ok {
				em.emit(ctx, waitingFor(parked.Hook, parked.Reason))
				writeRunState(opts.OutDir, parked)
				return nil, parked
			}
			if aerr != nil {
				em.emit(ctx, Event{Phase: phase.Name, State: EventFailed,
					Narration: narrate(EventFailed, startMeta),
					Detail:    aerr.Error()})
				return nil, fmt.Errorf("phase %q: %w", phase.Name, aerr)
			}
			meta := phaseMeta(decl, plan.byPhase[phase.Name])
			meta["elapsed"] = time.Since(started).Round(time.Second).String()
			if i+1 < len(phases) {
				meta["next"] = phases[i+1].Name
			}
			emitHookNarrations(ctx, em, phase.Name,
				narrationScope(phase.Name, title, hr.inputs, meta, hr.outputs),
				phase.Hooks.Await)
			em.emit(ctx, Event{Phase: phase.Name, State: EventDone,
				Narration: narrate(EventDone, meta),
				Meta:      meta})
		}
		// Nothing is waiting any more.
		clearRunState(opts.OutDir)
		// postInstantiate belongs to no phase, and says so: `{{ .phase.name }}`
		// there renders empty rather than silently carrying the last phase.
		hr.phase = Phase{}
		hr.resetOutputs()
		ran, herr := hr.run(ctx, bp.Hooks.PostInstantiate)
		if herr != nil {
			return nil, herr
		}
		hooksRun = append(hooksRun, ran...)
	}

	files := make([]string, 0, len(written))
	for p := range written {
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
