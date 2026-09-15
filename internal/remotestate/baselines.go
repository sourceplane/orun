package remotestate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// The baseline registry, as the CLI reads it (orun-bootstrap-engine BE-O7).
//
// # Why this file exists
//
// Before it, the binary had NO baseline verbs at all: every registry surface
// was console-or-API, so the "one command" story broke at step one — an
// operator had to open a console to learn an id and a tag before the CLI could
// do anything with either.
//
// Each method maps 1:1 onto a route the platform already serves. Nothing here
// decides anything: the paid gate, the admin grant and the bootstrap session
// live behind one door on the server, and a CLI that re-derived any of them
// would be a second answer to a question the platform already answers.

// Baseline is one registry row, as the platform serves it.
type Baseline struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Summary         string   `json:"summary"`
	Visibility      string   `json:"visibility"`
	Tier            string   `json:"tier"`
	SourceRepo      string   `json:"sourceRepo"`
	Tag             string   `json:"tag"`
	ExpectedMinutes int      `json:"expectedMinutes"`
	Stack           []string `json:"stack"`
	Requires        []string `json:"requires"`
	RetiredAt       string   `json:"retiredAt,omitempty"`
}

// Retired reports whether the row has been withdrawn. A retired baseline is
// still NAMED — a workspace built from one has to be able to say what it is —
// but it is not offered.
func (b Baseline) Retired() bool { return strings.TrimSpace(b.RetiredAt) != "" }

// ReadinessProvider is one provider a blueprint needs and whether it is there.
type ReadinessProvider struct {
	Provider  string `json:"provider"`
	Connected bool   `json:"connected"`
}

// BlueprintView is a baseline resolved for a workspace: what it is, plus
// whether that workspace could build it right now.
type BlueprintView struct {
	Blueprint struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Summary    string `json:"summary"`
		SourceRepo string `json:"sourceRepo"`
		Tag        string `json:"tag"`
		// ManifestPath is the build contract's path INSIDE SourceRepo, served
		// since orun-cloud BE-K1f. Without it a caller holds a repo and a tag
		// and still cannot find the contract in them: `blueprint.yaml` is the
		// convention every row but one follows, and `stratus-coolify` is that
		// one — two rows over a single tree, each with its own manifest — so a
		// caller that guesses serves the Azure contract to a Coolify build.
		//
		// Empty against a platform that predates BE-K1f, which is why every
		// reader here treats it as a fact to check rather than one to assume.
		ManifestPath    string `json:"manifestPath"`
		ExpectedMinutes int    `json:"expectedMinutes"`
		Readiness       struct {
			IntegrationsReady bool                `json:"integrationsReady"`
			Integrations      []ReadinessProvider `json:"integrations"`
		} `json:"readiness"`
	} `json:"blueprint"`
	// PinnedTag and PinnedTagStale are set when the caller addressed `id@tag`
	// and the registry has since moved. A stale pin RESOLVES — the visitor
	// clicking a link from a blog post wants to build the platform, not to
	// litigate a version — and is reported so a surface can say so.
	PinnedTag      string `json:"pinnedTag,omitempty"`
	PinnedTagStale bool   `json:"pinnedTagStale,omitempty"`
}

// ListPublicBaselines reads the public catalogue. No workspace, no session.
func (c *Client) ListPublicBaselines(ctx context.Context) ([]Baseline, error) {
	var resp struct {
		Baselines []Baseline `json:"baselines"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/baselines", nil, &resp, true); err != nil {
		return nil, err
	}
	return resp.Baselines, nil
}

// ListBaselines reads what a workspace's account may see: the public set plus
// anything that account registered itself.
func (c *Client) ListBaselines(ctx context.Context, org string) ([]Baseline, error) {
	var resp struct {
		Baselines []Baseline `json:"baselines"`
	}
	path := "/v1/organizations/" + urlSegment(org) + "/baselines"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return resp.Baselines, nil
}

// GetBlueprint resolves one baseline for a workspace, with readiness. The id
// may be `id` or `id@tag`.
func (c *Client) GetBlueprint(ctx context.Context, org, id string) (*BlueprintView, error) {
	var view BlueprintView
	path := fmt.Sprintf("/v1/organizations/%s/agents/blueprints/%s", urlSegment(org), urlSegment(id))
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &view, true); err != nil {
		return nil, err
	}
	return &view, nil
}

// BootstrapRequest starts a platform-run build.
type BootstrapRequest struct {
	RepoLinkID string            `json:"repoLinkId"`
	Inputs     map[string]string `json:"inputs,omitempty"`
	ProfileID  string            `json:"profileId,omitempty"`
}

// BootstrapResult is what the door hands back.
type BootstrapResult struct {
	SessionID  string `json:"sessionId"`
	SessionURL string `json:"sessionUrl"`
}

// Bootstrap asks the platform to start a build.
//
// Everything that makes this privileged stays on the server: the human-admin
// requirement, the paid gate, readiness, the repo grounding, and the
// time-boxed grant. This is a request, not a decision.
func (c *Client) Bootstrap(ctx context.Context, org, id string, req BootstrapRequest) (*BootstrapResult, error) {
	var out BootstrapResult
	path := fmt.Sprintf("/v1/organizations/%s/agents/blueprints/%s/bootstrap", urlSegment(org), urlSegment(id))
	if err := c.doJSON(ctx, http.MethodPost, path, req, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
