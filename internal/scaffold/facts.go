package scaffold

import (
	"context"
	"strings"

	"github.com/sourceplane/orun/internal/actions"
)

// Facts on the build's stream (orun-cloud saas-build-panel BV3).
//
// An action says what it made (`actions.Fact`, from its own outputs). The
// engine scopes each fact to the hook that filed it, applies the hook's
// overrides, and sends it as a `fact` event — only when it changed, so a
// pull request that sits at "checks running" for forty minutes is one event,
// not one a poll.
//
// The payload, all strings in Meta (the platform keeps string values only):
//
//	fact     "1" — the payload's version
//	key      identity; a later fact with the same key replaces the earlier
//	kind     repo | epic | pr | url | run
//	label    what a reader calls it
//	value    owner/name, EP-3, #12, a URL, a run id
//	href     where a reader goes
//	env      a live address's environment
//	status   where it stands (see actions.Fact)
//	private  "true" | "false", a repository's visibility when known
//	pin      "false" keeps it off a reader's pinned list
//
// A fact is never sent after its phase's `failed` or `waiting`: a consumer
// reads the phase's last transition as where it stands, and the facts a
// failing hook had (the pull request whose checks failed) are sent before
// the failure is.

// factVersion is the payload's version, `meta.fact`.
const factVersion = "1"

// hookFacts are the facts a hook files for these outputs, with the hook's
// own overrides applied: `quiet` files none, `label` names them, `pin`
// keeps them in view or off it. Keys an action left to the engine are scoped
// to the phase and the hook, so two landings in one build are two rows.
func hookFacts(phase string, h Hook, params map[string]any, out map[string]string, failed bool) []actions.Fact {
	if h.Quiet || !h.IsAction() {
		return nil
	}
	facts := actions.FactsOf(h.Uses, params, out, failed)
	for i := range facts {
		f := &facts[i]
		if f.Key == "" {
			scope := h.ID
			if phase != "" {
				scope = phase + "/" + h.ID
			}
			f.Key = f.Kind + ":" + scope
		}
		if label := strings.TrimSpace(h.Label); label != "" {
			f.Label = label
		}
		if h.Pin != nil {
			f.Unpinned = !*h.Pin
		}
	}
	return facts
}

// factMeta is a fact's payload. Empty fields are left out.
func factMeta(f actions.Fact) map[string]string {
	meta := map[string]string{"fact": factVersion, "key": f.Key, "kind": f.Kind}
	for k, v := range map[string]string{
		"label": f.Label, "value": f.Value, "href": f.Href, "env": f.Env, "status": f.Status,
	} {
		if v = strings.TrimSpace(v); v != "" {
			meta[k] = v
		}
	}
	if f.Private != nil {
		if *f.Private {
			meta["private"] = "true"
		} else {
			meta["private"] = "false"
		}
	}
	if f.Unpinned {
		meta["pin"] = "false"
	}
	return meta
}

// factSignature is what "changed" compares.
func factSignature(meta map[string]string) string {
	var b strings.Builder
	for _, k := range []string{"kind", "label", "value", "href", "env", "status", "private", "pin"} {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(meta[k])
		b.WriteByte(';')
	}
	return b.String()
}

// fact sends one fact under phase and step, unless the last one sent for its
// key said the same.
func (e *emitter) fact(ctx context.Context, phase, step string, f actions.Fact) {
	if e == nil || e.sink == nil || strings.TrimSpace(f.Key) == "" {
		return
	}
	meta := factMeta(f)
	sig := factSignature(meta)
	e.mu.Lock()
	if e.facts == nil {
		e.facts = map[string]string{}
	}
	if e.facts[f.Key] == sig {
		e.mu.Unlock()
		return
	}
	e.facts[f.Key] = sig
	e.mu.Unlock()
	e.emit(ctx, Event{Phase: phase, Step: step, State: EventFact, Meta: meta})
}

// facts sends a hook's facts.
func (e *emitter) hookFacts(ctx context.Context, phase string, h Hook, params map[string]any, out map[string]string, failed bool) {
	for _, f := range hookFacts(phase, h, params, out, failed) {
		e.fact(ctx, phase, h.ID, f)
	}
}
