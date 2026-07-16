package data

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/remotestate"
)

// CloudLane is the cockpit's cloud read lane (specs/orun-tui-v2 §10):
// org-scoped platform reads that merge into the same lists the local lane
// fills. It is deliberately not a Source — connection is a status, and
// the lane only ever adds rows.
type CloudLane struct {
	client *remotestate.Client
	org    string
	scope  string
}

// cloudRunTag marks cloud-lane run rows so surfaces can badge provenance
// and skip local drilldowns that cannot resolve them.
const cloudRunTag = "cloud:"

// IsCloudRun reports whether a run row came from the cloud lane.
func IsCloudRun(execID string) bool {
	return len(execID) > len(cloudRunTag) && execID[:len(cloudRunTag)] == cloudRunTag
}

// ResolveCloud builds the cloud lane from the environment and the stored
// CLI credential. Signed out, misconfigured, or offline all return nil —
// never an error a caller must handle; the cockpit renders local-only and
// the settings overlay says why (degradation law, design §10).
func ResolveCloud(ctx context.Context, version string) *CloudLane {
	backend := os.Getenv("ORUN_BACKEND_URL")
	org := firstEnv("ORUN_WORKSPACE", "ORUN_ORG")
	if backend == "" || org == "" {
		return nil
	}
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL: backend,
		Version:    version,
		Org:        org,
	})
	if err != nil || tokenSrc == nil {
		return nil
	}
	scope := org
	if prj := os.Getenv("ORUN_PROJECT"); prj != "" {
		scope += "/" + prj
	}
	return &CloudLane{
		client: remotestate.NewClient(backend, version, tokenSrc),
		org:    org,
		scope:  scope,
	}
}

// Scope names the lane for the header ("org_ab12/prj_cd34").
func (c *CloudLane) Scope() string { return c.scope }

// platformRunRow is the loose shape of one …/state/runs row. Fields are
// parsed defensively: the page is a versioned cloud contract and the lane
// must degrade to fewer columns, never to a broken frame.
type platformRunRow struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	Status    string `json:"status"`
	PlanName  string `json:"planName"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Runs reads the org runs feed, mapped into the shared run-list view with
// provenance-tagged exec ids.
func (c *CloudLane) Runs(ctx context.Context) (viewmodel.RunListView, error) {
	if c == nil || c.client == nil {
		return viewmodel.RunListView{}, context.Canceled
	}
	page, err := c.client.ListOrgRuns(ctx, c.org, remotestate.OrgRunsQuery{})
	if err != nil {
		return viewmodel.RunListView{}, err
	}
	return parseCloudRuns(page.Data), nil
}

// parseCloudRuns folds a runs page body into the view. Split out so the
// contract fixture tests exercise exactly what production parses.
func parseCloudRuns(data []byte) viewmodel.RunListView {
	var rows []platformRunRow
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		// Some pages wrap rows one level down; try {"runs": [...]}.
		var wrapped struct {
			Runs []platformRunRow `json:"runs"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return viewmodel.RunListView{}
		}
		rows = wrapped.Runs
	}
	var out viewmodel.RunListView
	for _, r := range rows {
		id := r.RunID
		if id == "" {
			id = r.ID
		}
		if id == "" {
			continue
		}
		name := r.PlanName
		if name == "" {
			name = r.Name
		}
		out.Runs = append(out.Runs, viewmodel.RunSummary{
			ExecID:    cloudRunTag + id,
			PlanName:  name,
			Status:    r.Status,
			StartedAt: parseWhen(r.StartedAt, r.CreatedAt),
		})
	}
	return out
}

func parseWhen(candidates ...string) time.Time {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, c); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// WithCloud composes a local Source with the cloud lane: cloud rows merge
// into Runs (newest first is preserved per lane, cloud appended); every
// other read passes through. Lane errors keep the local result — the lane
// vanishing must never disturb local lanes (invariant §13.6).
func WithCloud(local Source, lane *CloudLane) Source {
	if lane == nil {
		return local
	}
	return &mergedSource{Source: local, lane: lane}
}

type mergedSource struct {
	Source
	lane *CloudLane
}

// Scope implements Source: the header shows the org scope when connected.
func (m *mergedSource) Scope() string { return m.lane.Scope() }

// Capabilities implements Source.
func (m *mergedSource) Capabilities() Caps {
	caps := m.Source.Capabilities()
	caps.Remote = true
	return caps
}

// Runs implements Source: local first, then the cloud lane, silently
// dropped on error.
func (m *mergedSource) Runs(ctx context.Context) (viewmodel.RunListView, error) {
	local, err := m.Source.Runs(ctx)
	if err != nil {
		// A broken local lane still shows cloud rows rather than nothing.
		local = viewmodel.RunListView{}
	}
	if cloud, cerr := m.lane.Runs(ctx); cerr == nil {
		local.Runs = append(local.Runs, cloud.Runs...)
	}
	return local, nil
}
