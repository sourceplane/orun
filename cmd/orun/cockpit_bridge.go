package main

import (
	"io"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/cockpit/render"
	"github.com/sourceplane/orun/internal/cockpit/surface"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/ui"
)

// cockpitSurface returns the right Surface for the current stdout. Honours
// NO_COLOR and non-TTY automatically via ui.ColorEnabledForWriter.
func cockpitSurface(w io.Writer) surface.Surface {
	if f, ok := w.(*os.File); ok {
		if ui.ColorEnabledForWriter(f) {
			return surface.ANSI(f)
		}
		return surface.Plain(f)
	}
	return surface.Plain(w)
}

// cockpitRenderExecution paints a single execution using the cockpit
// render pipeline. Returns (rendered, error). If rendered is false, the
// caller should fall back to the legacy renderer (e.g. for JSON callers
// that need shape-specific output).
func cockpitRenderExecution(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState) (bool, error) {
	s := cockpitSurface(os.Stdout)
	v := viewmodel.BuildRunView(execID, meta, st)
	lines := render.RunStatus(s, v)
	for _, line := range lines {
		if _, err := io.WriteString(os.Stdout, line+"\n"); err != nil {
			return true, err
		}
	}
	return true, nil
}

// cockpitRenderRunList paints the all-executions table.
func cockpitRenderRunList(entries []execmodel.ExecEntry) error {
	s := cockpitSurface(os.Stdout)
	view := viewmodel.BuildRunListView(entries)
	lines := render.RunList(s, view)
	for _, line := range lines {
		if _, err := io.WriteString(os.Stdout, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// statusIsTerminal returns true when meta.Status is a terminal state.
// Centralised so watch loops in cockpit + legacy callers agree.
func statusIsTerminal(meta *execmodel.ExecMetadata) bool {
	if meta == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(meta.Status))
	return s == "completed" || s == "failed"
}
