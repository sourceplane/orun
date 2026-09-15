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

// ── Repo links (orun-bootstrap-engine BE-O7b) ──────────────────────────────

// RepoLink is one of a workspace's linked repositories, as the platform serves
// it. Only the fields a build needs: what it IS, whether an agent may write to
// it, and the id every door takes.
type RepoLink struct {
	ID            string `json:"id"`
	RepoFullName  string `json:"repoFullName"`
	DefaultBranch string `json:"defaultBranch"`
	Status        string `json:"status"`
	AgentAccess   string `json:"agentAccess"`
}

// ListRepoLinks reads the workspace's linked repositories, across every
// project.
//
// This exists because a repo link id is the ONE fact a platform-run build
// needs that a person standing in a repository does not have: they know the
// repository, and the platform knows the `repl_…` that names it here. BE-O7
// deferred `--via-platform` for exactly this, calling it "a repo link id the
// CLI has no verb to resolve" — the resolution is this list plus a name match,
// and it belongs in the binary rather than in a person's clipboard.
func (c *Client) ListRepoLinks(ctx context.Context, org string) ([]RepoLink, error) {
	var resp struct {
		RepoLinks []RepoLink `json:"repoLinks"`
	}
	path := "/v1/organizations/" + urlSegment(org) + "/repo-links"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return resp.RepoLinks, nil
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

// ── Register (orun-bootstrap-engine BE-O7b) ────────────────────────────────

// RegisterBaselineRequest is what an account may write about its own baseline.
//
// `visibility` is deliberately absent from the happy path of this struct's
// doc: an account may say `private` or `unlisted` and the door refuses
// `public` outright, because a public baseline is a repo an agent clones into
// a stranger's workspace — so it is the platform's to grant, not a field.
//
// The two SHELL paths are a pair. A baseline with a brief is run through its
// umbrella; a BLUEPRINT-DRIVEN one has neither and its build document is the
// build. Omitting both is how you say the second, and declaring one of the two
// is refused by the door as half a shell layer.
type RegisterBaselineRequest struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Summary         string   `json:"summary,omitempty"`
	Stack           []string `json:"stack,omitempty"`
	SourceRepo      string   `json:"sourceRepo"`
	Tag             string   `json:"tag"`
	Requires        []string `json:"requires,omitempty"`
	ExpectedMinutes int      `json:"expectedMinutes"`
	Visibility      string   `json:"visibility,omitempty"`
	BriefPath       *string  `json:"briefPath,omitempty"`
	UmbrellaPath    *string  `json:"umbrellaPath,omitempty"`
	// ManifestPath names the build contract inside the source repo. Omitted
	// leaves the platform's conventional default; named when a repository
	// holds two baselines over one tree, which is what `stratus-coolify` is.
	ManifestPath string `json:"manifestPath,omitempty"`
}

// RegisterBaselineResult is the row the platform created.
type RegisterBaselineResult struct {
	Baseline Baseline `json:"baseline"`
}

// RegisterBaseline adds a baseline under the caller's account.
//
// NOT RETRYABLE: the door refuses a duplicate id with a 409 carrying the
// existing row, which is the allocator posture — a blind retry of a request
// that timed out after the row was created would report that conflict as a
// failure of the thing that in fact succeeded.
func (c *Client) RegisterBaseline(
	ctx context.Context,
	org string,
	req RegisterBaselineRequest,
) (*RegisterBaselineResult, error) {
	var out RegisterBaselineResult
	path := "/v1/organizations/" + urlSegment(org) + "/baselines"
	if err := c.doJSON(ctx, http.MethodPost, path, req, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Publish (orun-bootstrap-engine BE-O7b) ─────────────────────────────────

// PublishResult is what the door hands back once the tag is proven.
type PublishResult struct {
	Baseline     Baseline `json:"baseline"`
	PublishedTag string   `json:"publishedTag"`
}

// PublishBaseline moves a registered baseline to a tag, once the platform has
// proven the tag carries what a build reads.
//
// The ORDER is what this verb exists to remove. A baseline's tag lives in two
// repositories — the git tag in the source repo, and the pin in the registry —
// and getting them the wrong way round means every build of that baseline 404s
// until somebody notices. Push the tag, then run this: the door refuses a pin
// that is a branch, a tag missing the files a build enters through, or a build
// contract that does not parse, and the registry simply does not move.
//
// NOTHING IS DECIDED HERE. The CLI does not pre-check the tag and it must not:
// a client that formed its own opinion would be a second answer to a question
// the platform already answers, and the two would drift the first time the
// contract changed. This is a request.
func (c *Client) PublishBaseline(ctx context.Context, org, id, tag string) (*PublishResult, error) {
	var out PublishResult
	body := struct {
		Tag string `json:"tag"`
	}{Tag: tag}
	path := fmt.Sprintf(
		"/v1/organizations/%s/baselines/%s/publish",
		urlSegment(org), urlSegment(id),
	)
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── The build event stream (orun-bootstrap-engine BE-O13) ──────────────────

// BuildEvent is one `bootstrap-event/v1` object as the platform's ingest door
// takes it. The engine's own `scaffold.Event` is the source; this is the wire
// shape, kept separate so a change to one is a deliberate change to the other.
//
// `schema` and `runId` are deliberately ABSENT. The door takes the run id from
// the path and has no use for the envelope version — a body that carried its
// own would be two sources of truth for one fact, with the tenant boundary on
// the losing side.
type BuildEvent struct {
	Seq       int               `json:"seq"`
	At        string            `json:"at"`
	Phase     string            `json:"phase,omitempty"`
	Step      string            `json:"step,omitempty"`
	State     string            `json:"state"`
	Narration string            `json:"narration,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// AppendBuildEventsResult is what the door reports back. `LatestSeq` is the
// acknowledgement that matters: it is the highest seq the platform now holds,
// and the next batch must start at or below it plus one.
type AppendBuildEventsResult struct {
	Appended  int `json:"appended"`
	Received  int `json:"received"`
	LatestSeq int `json:"latestSeq"`
}

// AppendBuildEvents delivers a batch of what the engine emitted.
//
// NOT RETRYABLE at this layer, deliberately. The door is idempotent by
// (org, run, seq), so re-sending is safe — but it also REFUSES a batch that
// would leave a gap, and the caller is the only thing that knows which seq it
// has had acknowledged. A blind transport retry that raced a partial success
// would be re-sending the wrong window; the sender above this retries from its
// own high-water mark instead, which is the only place that fact lives.
func (c *Client) AppendBuildEvents(
	ctx context.Context,
	org, runID string,
	events []BuildEvent,
) (*AppendBuildEventsResult, error) {
	var out AppendBuildEventsResult
	body := struct {
		Events []BuildEvent `json:"events"`
	}{Events: events}
	path := fmt.Sprintf(
		"/v1/organizations/%s/agents/builds/%s/events",
		urlSegment(org), urlSegment(runID),
	)
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
