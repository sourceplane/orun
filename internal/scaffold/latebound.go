package scaffold

import (
	"fmt"
	"sort"
	"strings"
)

// Late-bound inputs (orun-cloud saas-orun-work WA5, design §5.8).
//
// # What they are for
//
// orun.work builds an app from a baseline in two stages. The SCAFFOLD stage
// needs GitHub only: the code placed and branded, a repository created and
// landed. The LIVE stage needs the baseline's providers: credentials
// reconciled, deployed, probed. An account with no Cloudflare connection
// stops after scaffold and has a code-only app; a later build runs the rest.
//
// Some inputs cannot be known before the live stage — a workers.dev subdomain
// is a fact of the Cloudflare account the app will deploy into, and the
// account that has not connected one has no subdomain to give. Such an input
// is declared `lateBound: true`. Its `default` is the placeholder the scaffold
// stage writes, which "the live stage must replace and may never keep".
//
// # The three rules, and where each lives
//
//  1. A run stopped before the build's end may keep the placeholder. Only
//     `--until <phase>` stops a build, and only when <phase> is not the last
//     one: `--until` the last phase places everything.
//  2. Every other run must be given a value. A full build, a resume with no
//     stop, `--phase` and `--until` the last phase are refused before
//     anything is placed (checkLateBound, called from Run). `--phase` counts
//     as reaching the end because it is how the platform runs the live stage
//     on its own (`phase: <live phase>`), and orun does not know which of a
//     build document's phases are live: refusing a scaffold-only `--phase`
//     that has no value costs a clear error, while letting a live one through
//     deploys the placeholder.
//  3. A resumed run takes a given value over the recorded one (the scaffold
//     recorded the placeholder; the live stage brings the real thing), and a
//     recorded placeholder is not recovered as though it were a value
//     (RecoverInputs). Rule 2 then sees a live run that forgot the value as
//     what it is.
//
// A value someone gives explicitly is theirs, even when it equals the
// placeholder: the rule is about a value nobody supplied, not about policing
// what the operator typed.

// validateLateBound checks one input's `lateBound` against the rest of its
// declaration. phases is how many phases the blueprint declares.
func validateLateBound(name string, in InputSpec, phases int) error {
	if !in.LateBound {
		return nil
	}
	// The record holds a secret as `<secret>`, never its value, so neither a
	// resume that takes a new value over the recorded one nor a check that
	// the placeholder was replaced could work on it. Credentials a later
	// phase needs are minted by its hooks (orun.integrations/reconcile@v1),
	// not passed in as inputs.
	if in.Secret {
		return fmt.Errorf("input %q: lateBound and secret do not combine — a secret is never recorded, so there is no placeholder to replace and nothing for a resumed run to replace it in; mint a credential a later phase needs in that phase's hooks", name)
	}
	// The default IS the placeholder. Without one, a stopped build has
	// nothing to write in the value's place and the rest of the build
	// nothing to recognise as kept.
	if in.Default == nil {
		return fmt.Errorf("input %q: lateBound needs a default — the placeholder a build that stops before the value exists writes in its place (e.g. default: your-workers-subdomain)", name)
	}
	// Late is relative to a phase boundary. With fewer than two phases there
	// is no phase to stop before, so every run would have to supply it and
	// the declaration would promise something the build cannot do.
	if phases < 2 {
		return fmt.Errorf("input %q: lateBound needs at least two phases — without a phase to stop before, there is no later phase to bind it in", name)
	}
	return nil
}

// stopsEarly reports whether `--until <until>` stops this build before its
// last declared phase — the only kind of run that may keep a late-bound
// placeholder. A name the blueprint does not declare answers true: the phase
// selection refuses it by name, and this must not get there first with a
// complaint about an input.
func (bp *Blueprint) stopsEarly(until string) bool {
	until = strings.TrimSpace(until)
	if until == "" || len(bp.Phases) == 0 {
		return false
	}
	for i, ph := range bp.Phases {
		if ph.Name == until {
			return i < len(bp.Phases)-1
		}
	}
	return true
}

// checkLateBound refuses a run that would keep a late-bound input's
// placeholder (rule 2). inputs are the raw values the run was given, after
// recovery: a key absent from them falls to its default, which for a
// late-bound input is the placeholder.
func checkLateBound(bp *Blueprint, opts Options) error {
	if bp.stopsEarly(opts.Until) {
		return nil
	}
	var kept []string
	for name, spec := range bp.Inputs {
		if !spec.LateBound {
			continue
		}
		if _, given := opts.Inputs[name]; given {
			continue
		}
		kept = append(kept, name)
	}
	if len(kept) == 0 {
		return nil
	}
	sort.Strings(kept)
	parts := make([]string, 0, len(kept))
	sets := make([]string, 0, len(kept))
	for _, name := range kept {
		parts = append(parts, fmt.Sprintf("%s (placeholder %q)", name, fmt.Sprint(bp.Inputs[name].Default)))
		sets = append(sets, "--set "+name+"=…")
	}
	last := bp.Phases[len(bp.Phases)-1].Name
	return inputErr(
		"late-bound %s not given: %s.\n"+
			"Only a run --until stops before the build's last phase (%s) may keep a placeholder; this one %s, "+
			"and the phases that need the value would get the placeholder instead.\n"+
			"Pass %s (or put it in --values), or stop earlier with --until <phase>. Nothing has been written.",
		pluralWord(len(kept), "input", "inputs"), strings.Join(parts, ", "), last, reachWords(opts), strings.Join(sets, " "))
}

// reachWords says why a run counts as reaching the end, for the refusal.
func reachWords(opts Options) string {
	switch {
	case strings.TrimSpace(opts.Only) != "":
		return "runs --phase " + opts.Only + ", which may be the stage that needs it"
	case strings.TrimSpace(opts.Until) != "":
		return "runs --until " + opts.Until + ", which is every phase"
	default:
		return "runs through it"
	}
}

// isPlaceholder reports whether a recorded value is a late-bound input's
// placeholder — what the product was waiting for, not what it was built with.
func isPlaceholder(spec InputSpec, recorded any) bool {
	return spec.LateBound && spec.Default != nil && fmt.Sprint(recorded) == fmt.Sprint(spec.Default)
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
