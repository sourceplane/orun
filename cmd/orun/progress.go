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
