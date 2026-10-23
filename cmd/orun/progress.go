package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sourceplane/orun/internal/scaffold"
)

// Rendering the build event stream (orun-bootstrap-engine BE-O6).
//
// One stream, four renderings. They differ only in what they SHOW, never in
// what happened — which is what makes `--progress json` a thing a baseline's
// end-to-end CI can assert the operator-visible sequence against.

type progressMode string

const (
	progressAuto    progressMode = "auto"    // narration, with facts
	progressPlain   progressMode = "plain"   // narration only — CI logs
	progressVerbose progressMode = "verbose" // narration + every detail line
	progressJSON    progressMode = "json"    // the raw stream
)

func parseProgressMode(s string) (progressMode, error) {
	switch progressMode(strings.TrimSpace(s)) {
	case "", progressAuto:
		return progressAuto, nil
	case progressPlain:
		return progressPlain, nil
	case progressVerbose:
		return progressVerbose, nil
	case progressJSON:
		return progressJSON, nil
	}
	return "", fmt.Errorf("--progress must be auto, plain, verbose or json; got %q", s)
}

// progressSink renders events to a writer.
type progressSink struct {
	out  io.Writer
	mode progressMode
}

func (p *progressSink) Emit(_ context.Context, e scaffold.Event) {
	if p.mode != progressJSON && runOwnEvent(e) {
		// The run's own start and end are for a platform holding something
		// for the build (a repository's lease); a terminal already says
		// what it is building and how it ended.
		return
	}
	switch p.mode {
	case progressJSON:
		enc := json.NewEncoder(p.out)
		_ = enc.Encode(e)
		return
	case progressPlain:
		if e.Narration != "" {
			fmt.Fprintln(p.out, e.Narration)
		}
		return
	}

	// A fact is a record, not a transition: it has no narration, and its
	// row sits under the line of the step that made it.
	if e.State == scaffold.EventFact {
		if row := factRow(e); row != "" {
			fmt.Fprintf(p.out, "    %s\n", row)
		}
		return
	}

	mark := map[scaffold.EventState]string{
		scaffold.EventStarted: "→", scaffold.EventDone: "✓",
		scaffold.EventFailed: "✕", scaffold.EventWaiting: "…",
		scaffold.EventSkipped: "–", scaffold.EventRunning: "·",
	}[e.State]
	if e.Narration != "" {
		fmt.Fprintf(p.out, "%s %s\n", mark, e.Narration)
	}
	// Facts sit under the prose: the YAML supplies the words, the engine
	// supplies the numbers, and a reader can tell which is which.
	if facts := factLine(e); facts != "" {
		fmt.Fprintf(p.out, "    %s\n", facts)
	}
	if p.mode == progressVerbose && e.Detail != "" {
		fmt.Fprintf(p.out, "    %s\n", e.Detail)
	}
}

// factLine renders the engine's own numbers, in a stable order so two runs of
// the same blueprint produce the same output.
func factLine(e scaffold.Event) string {
	if len(e.Meta) == 0 || e.State != scaffold.EventDone {
		return ""
	}
	var parts []string
	for _, key := range []string{"files", "elapsed", "expectedMinutes"} {
		if v, ok := e.Meta[key]; ok && v != "" && v != "0" {
			switch key {
			case "files":
				parts = append(parts, v+" file(s) placed")
			case "elapsed":
				parts = append(parts, "took "+v)
			case "expectedMinutes":
				parts = append(parts, "budget "+v+"m")
			}
		}
	}
	return strings.Join(parts, " · ")
}

// factRow is one fact the build made, as a terminal shows it: the
// repository, the epic, each pull request and where it stands, each address
// that answered. A fact marked unpinned (a health check, a convergence run)
// is the build's own record and is not shown.
func factRow(e scaffold.Event) string {
	m := e.Meta
	if m["pin"] == "false" {
		return ""
	}
	value := strings.TrimSpace(m["value"])
	label := strings.TrimSpace(m["label"])
	with := func(s string) string {
		if label != "" {
			return s + " — " + label
		}
		return s
	}
	switch m["kind"] {
	case "repo":
		if value == "" {
			return ""
		}
		return "repository github.com/" + value
	case "epic":
		if value == "" {
			return ""
		}
		return with("epic " + value)
	case "pr":
		words := map[string]string{
			"opening": "opening", "checks": "checks running", "failing": "checks failing",
			"open": "open", "merged": "merged", "closed": "closed",
		}[m["status"]]
		if value == "" {
			value = "pull request"
		} else {
			value = "pull request " + value
		}
		if words != "" {
			value += " · " + words
		}
		return with(value)
	case "url":
		if value == "" || m["status"] == "unreachable" {
			return ""
		}
		if env := strings.TrimSpace(m["env"]); env != "" {
			return with(env + " " + value)
		}
		return with(value)
	}
	return ""
}

// runOwnEvent reports whether e is the run's own start or end: no phase, no
// step, started, done or failed.
func runOwnEvent(e scaffold.Event) bool {
	if e.Phase != "" || e.Step != "" {
		return false
	}
	switch e.State {
	case scaffold.EventStarted, scaffold.EventDone, scaffold.EventFailed:
		return true
	}
	return false
}
