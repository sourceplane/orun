package remotestate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Epics client (orun-baseline-tracking BT-O2) — the task plane's containers:
// `/v1/organizations/{org}/tasks/epics…` through the api-edge facade. An
// epic is the programme tasks and milestones club under; a milestone is a
// PHASE of a native epic, ordered by `after`, carrying exit criteria. Every
// type mirrors orun-cloud packages/contracts/src/tasks.ts (E1, TV2, W1/W2);
// the rollup is derived at read on the server (TK-M) — nothing here caches
// or stores one. A tracker-mirrored epic (provider != "") is the tracker's:
// its phases refuse authoring here with 412 `mirrored`.

// PublicEpic mirrors PublicEpic on the wire. Nullable fields decode to "".
type PublicEpic struct {
	// ID is the durable epc_… handle; Slug the human one; Key the minted
	// EP-n (empty only for a row the backfill has not reached).
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Key  string `json:"key"`
	Name string `json:"name"`
	// State is the tracker's word on a mirrored epic, orun's on a native
	// one; StateCategory the portable category both speak.
	State         string `json:"state"`
	StateCategory string `json:"stateCategory"`
	TargetDate    string `json:"targetDate"`
	Description   string `json:"description"`
	Owner         string `json:"owner"`
	// Provider is the tracker binding, as data — "" for orun-native epics.
	Provider   string `json:"provider"`
	URL        string `json:"url"`
	ArchivedAt string `json:"archivedAt"`
	CreatedAt  string `json:"createdAt"`
	// Health is the ASSERTED health (TV3): somebody's word, or "".
	Health     string `json:"health"`
	HealthNote string `json:"healthNote"`
}

// PublicMilestone mirrors PublicMilestone: a phase of its epic.
type PublicMilestone struct {
	ID           string   `json:"id"`
	EpicID       string   `json:"epicId"`
	Name         string   `json:"name"`
	TargetDate   string   `json:"targetDate"`
	SortOrder    float64  `json:"sortOrder"`
	ExitCriteria []string `json:"exitCriteria"`
}

// EpicCreateRequest mirrors CreateEpicRequest — an orun-native epic. The
// slug is minted from the name when omitted; a taken slug is refused with
// the holder (409, details.existing), never silently suffixed.
type EpicCreateRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
	TargetDate  string `json:"targetDate,omitempty"`
	// Owner is a subject ref (usr_… / sp_…) or "me".
	Owner string `json:"owner,omitempty"`
}

// MilestoneCreateRequest mirrors CreateMilestoneRequest. Position is the
// spoken form: After names the sibling (mls_…) this phase follows; First
// puts it first (`after: null` on the wire); neither puts it last.
type MilestoneCreateRequest struct {
	Name         string
	TargetDate   string
	ExitCriteria []string
	After        string
	First        bool
}

// MarshalJSON renders the three positions the server distinguishes: a
// sibling, an explicit null, or the key absent.
func (r MilestoneCreateRequest) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{"name": r.Name}
	if r.TargetDate != "" {
		m["targetDate"] = r.TargetDate
	}
	if r.ExitCriteria != nil {
		m["exitCriteria"] = r.ExitCriteria
	}
	switch {
	case r.After != "":
		m["after"] = r.After
	case r.First:
		m["after"] = nil
	}
	return json.Marshal(m)
}

// EpicMilestoneRollup is one phase's line of the rollup.
type EpicMilestoneRollup struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	TargetDate string   `json:"targetDate"`
	Total      int      `json:"total"`
	Done       int      `json:"done"`
	Assignees  []string `json:"assignees"`
}

// EpicRollup mirrors EpicRollup — folded from the children's verdicts AT
// READ. It renders beside the mirrored status as the second voice:
// `tracker: started · orun: 4/9 done`.
type EpicRollup struct {
	Total      int            `json:"total"`
	Rungs      map[string]int `json:"rungs"`
	Done       int            `json:"done"`
	Blocked    int            `json:"blocked"`
	Governance struct {
		Own       int `json:"own"`
		Inherited int `json:"inherited"`
		None      int `json:"none"`
	} `json:"governance"`
	Milestones []EpicMilestoneRollup `json:"milestones"`
}

// EpicView mirrors GetEpicResponse (with `?include=rollup`): the epic, its
// phases in order, the task count, and the derived rollup.
type EpicView struct {
	Epic       PublicEpic        `json:"epic"`
	Milestones []PublicMilestone `json:"milestones"`
	TaskCount  int               `json:"taskCount"`
	Rollup     *EpicRollup       `json:"rollup,omitempty"`
}

// EpicsList mirrors ListEpicsResponse.
type EpicsList struct {
	Epics []PublicEpic `json:"epics"`
}

func epicsPathFor(org, suffix string) string {
	return orgPath(org, "/tasks/epics"+suffix)
}

// CreateEpic creates an orun-native epic. Not retried: a second attempt
// against a now-taken slug is the 409 the caller reads the holder from
// (ExistingEpicOf).
func (c *Client) CreateEpic(ctx context.Context, org string, req EpicCreateRequest) (*PublicEpic, error) {
	var resp struct {
		Epic PublicEpic `json:"epic"`
	}
	if err := c.doJSON(ctx, http.MethodPost, epicsPathFor(org, ""), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp.Epic, nil
}

// ExistingEpicOf reads the holder a slug-taken 409 carries in its details
// (`{existing: PublicEpic}`); nil for any other error. The collision is the
// caller's decision — for an idempotent caller the decision is "that one".
func ExistingEpicOf(err error) *PublicEpic {
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusConflict || len(apiErr.Details) == 0 {
		return nil
	}
	var d struct {
		Existing *PublicEpic `json:"existing"`
	}
	if json.Unmarshal(apiErr.Details, &d) != nil || d.Existing == nil || d.Existing.ID == "" {
		return nil
	}
	return d.Existing
}

// ListEpics fetches the org's epics.
func (c *Client) ListEpics(ctx context.Context, org string) (*EpicsList, error) {
	var resp EpicsList
	if err := c.doJSON(ctx, http.MethodGet, epicsPathFor(org, ""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetEpic fetches one epic (epc_… id, EP-n key, or slug) with its phases
// and the derived rollup in the same read.
func (c *Client) GetEpic(ctx context.Context, org, ref string) (*EpicView, error) {
	var resp EpicView
	if err := c.doJSON(ctx, http.MethodGet, epicsPathFor(org, "/"+urlSegment(ref)+"?include=rollup"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateMilestone adds a phase to a native epic. Not retried: two phases
// of the same name are two phases (the server does not dedupe by name), so
// the caller lists first (GetEpic) when it means "ensure".
func (c *Client) CreateMilestone(ctx context.Context, org, epicRef string, req MilestoneCreateRequest) (*PublicMilestone, error) {
	var resp struct {
		Milestone PublicMilestone `json:"milestone"`
	}
	if err := c.doJSON(ctx, http.MethodPost, epicsPathFor(org, "/"+urlSegment(epicRef)+"/milestones"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp.Milestone, nil
}

// MilestoneView mirrors GetMilestoneResponse: the milestone with its epic,
// so a caller holding just the ref climbs to the container in one hop.
type MilestoneView struct {
	Milestone PublicMilestone `json:"milestone"`
	Epic      *PublicEpic     `json:"epic"`
}

// GetMilestone fetches one milestone (mls_…) with its epic.
func (c *Client) GetMilestone(ctx context.Context, org, ref string) (*MilestoneView, error) {
	var resp MilestoneView
	if err := c.doJSON(ctx, http.MethodGet, orgPath(org, "/tasks/milestones/"+urlSegment(ref)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ContainerContractView mirrors GetContainerContractResponse — the E3
// governing document on an epic or a milestone. The body stays raw bytes
// on this seam (internal/contract owns the type).
type ContainerContractView struct {
	ContractHash string          `json:"contractHash"`
	Contract     json.RawMessage `json:"contract"`
	AttachedAt   string          `json:"attachedAt"`
}

// GetContainerContract fetches the contract attached to a container
// ("epics" or "milestones") by ref. A 404 is an answer ("ungoverned"), which
// the caller reads with IsNotFound — never an error to surface.
func (c *Client) GetContainerContract(ctx context.Context, org, container, ref string) (*ContainerContractView, error) {
	var resp ContainerContractView
	path := orgPath(org, "/tasks/"+container+"/"+urlSegment(ref)+"/contract")
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// IsNotFound reports whether err is the platform's 404 (the not_found code
// or a bare 404 status).
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}
	return apiErr.Status == http.StatusNotFound || apiErr.Code == "not_found"
}

// ── Writes with an Idempotency-Key (the MCP rails, orun-mcp UM2) ─────────

// CreateEpicWithKey is CreateEpic under a caller-chosen Idempotency-Key: a
// retry under the same key replays the original result at the edge.
func (c *Client) CreateEpicWithKey(ctx context.Context, org string, req EpicCreateRequest, idemKey string) (*PublicEpic, error) {
	page, err := c.platformDo(ctx, http.MethodPost, epicsPathFor(org, ""), req, idemKey)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Epic PublicEpic `json:"epic"`
	}
	if err := json.Unmarshal(page.Data, &resp); err != nil {
		return nil, fmt.Errorf("decoding epic: %w", err)
	}
	return &resp.Epic, nil
}

// CreateMilestoneWithKey is CreateMilestone under a caller-chosen
// Idempotency-Key.
func (c *Client) CreateMilestoneWithKey(ctx context.Context, org, epicRef string, req MilestoneCreateRequest, idemKey string) (*PublicMilestone, error) {
	page, err := c.platformDo(ctx, http.MethodPost, epicsPathFor(org, "/"+urlSegment(epicRef)+"/milestones"), req, idemKey)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Milestone PublicMilestone `json:"milestone"`
	}
	if err := json.Unmarshal(page.Data, &resp); err != nil {
		return nil, fmt.Errorf("decoding milestone: %w", err)
	}
	return &resp.Milestone, nil
}
