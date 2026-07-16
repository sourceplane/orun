package data

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/sourceplane/orun/internal/worklens"
)

// EpicView is one sealed epic snapshot in the local Work lane
// (specs/orun-tui-v2 §5, Q2: offline Work shows sealed pulls with an
// honest banner; the live plane is the cloud lane, TR8).
type EpicView struct {
	// Slug is the .orun/epics/<slug> directory name.
	Slug string
	// Snapshot is the approval-sealed brief (worklens.EpicSnapshot).
	Snapshot worklens.EpicSnapshot
	// Brief is the rendered BRIEF.md body ("" when absent).
	Brief string
}

// loadEpics reads every sealed snapshot under <orunRoot>/epics.
func loadEpics(orunRoot string) ([]EpicView, error) {
	dir := filepath.Join(orunRoot, "epics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EpicView
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name(), "snapshot.json"))
		if rerr != nil {
			continue
		}
		var snap worklens.EpicSnapshot
		if json.Unmarshal(raw, &snap) != nil {
			continue
		}
		ev := EpicView{Slug: e.Name(), Snapshot: snap}
		if brief, berr := os.ReadFile(filepath.Join(dir, e.Name(), "BRIEF.md")); berr == nil {
			ev.Brief = string(brief)
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// Work implements Source for LocalSource: the sealed epic lane.
func (s *LocalSource) Work(context.Context) ([]EpicView, error) {
	return loadEpics(s.cfg.OrunRoot)
}

// Work implements Source for MockSource.
func (m *MockSource) Work(context.Context) ([]EpicView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]EpicView(nil), m.Epics...), m.Err
}
