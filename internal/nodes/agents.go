package nodes

import (
	"context"
	"strings"

	"github.com/sourceplane/orun/internal/objectstore"
)

// agents.go — the AG object kinds (specs/orun-agents/data-model.md). Agent
// types and agent sessions are content in the same graph as sources, catalogs,
// and specs:
//
//   - AgentTypeSnapshot: an agents/<name>.md sealed — frontmatter → this typed
//     capability envelope, persona body → a content-addressed blob (bodyRef),
//     bundled by a Merkle tree (identity = tree id, like CatalogSnapshot).
//   - AgentBrief: the frozen input a run starts from; blob identity.
//   - AgentSessionSegment: a chained slice of the append-only session event
//     log (prev-linked, tamper-evident); blob identity.
//   - AgentSessionSnapshot: the sealed run — every input pinned by hash; blob
//     identity. Wall-clock timestamps and cost counters are annotation, not
//     identity, and deliberately have no fields here (data-model.md §8).

// Agent run kinds (data-model.md §3).
const (
	RunKindDesign         = "design"
	RunKindImplementation = "implementation"
	RunKindInteractive    = "interactive"
	RunKindFix            = "fix"
)

func validRunKind(s string) bool {
	switch s {
	case RunKindDesign, RunKindImplementation, RunKindInteractive, RunKindFix:
		return true
	default:
		return false
	}
}

// Autonomy ladder rungs (agent-type-format.md §2).
const (
	AutonomyManual       = "manual"
	AutonomyAssist       = "assist"
	AutonomyAutoDispatch = "auto-dispatch"
	AutonomyFull         = "full"
)

func validAutonomy(s string) bool {
	switch s {
	case AutonomyManual, AutonomyAssist, AutonomyAutoDispatch, AutonomyFull:
		return true
	default:
		return false
	}
}

// Session event kinds — the closed vocabulary (data-model.md §3.3). Note what
// is absent: no status/lifecycle kind exists, so "agent asserts progress" is
// unrepresentable at the schema level.
const (
	SessionEventStateChanged      = "state_changed"
	SessionEventHarness           = "harness_event"
	SessionEventMessageUser       = "message_user"
	SessionEventMessageAgent      = "message_agent"
	SessionEventToolCall          = "tool_call"
	SessionEventToolResult        = "tool_result"
	SessionEventApprovalRequested = "approval_requested"
	SessionEventApprovalResolved  = "approval_resolved"
	SessionEventArtifactProduced  = "artifact_produced"
	SessionEventCostSample        = "cost_sample"
	SessionEventError             = "error"
)

// ValidSessionEventKind reports whether k is in the closed session-event
// vocabulary. Exported so the segment encoder and the runtime enforce one
// list.
func ValidSessionEventKind(k string) bool {
	switch k {
	case SessionEventStateChanged, SessionEventHarness, SessionEventMessageUser,
		SessionEventMessageAgent, SessionEventToolCall, SessionEventToolResult,
		SessionEventApprovalRequested, SessionEventApprovalResolved,
		SessionEventArtifactProduced, SessionEventCostSample, SessionEventError:
		return true
	default:
		return false
	}
}

// nameSegmentRe-equivalent: agent-type names share the tree-entry alphabet.
func validAgentName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return s != "." && s != ".."
}

// AgentRuntime is the model-tuning surface of an agent type. Floats are banned
// in canonical records, so temperature is carried as a string ("0", "0.2").
type AgentRuntime struct {
	Effort        string `json:"effort,omitempty"`
	Temperature   string `json:"temperature,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	ContextBudget int    `json:"contextBudget,omitempty"`
}

// AgentToolPolicy is the capability contract over MCP tools — deny-by-default.
type AgentToolPolicy struct {
	Allow []string `json:"allow,omitempty"`
	Ask   []string `json:"ask,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// AgentSecrets pins which secret:// reference globs the type may resolve.
type AgentSecrets struct {
	Use []string `json:"use,omitempty"`
}

// AgentTypeSnapshot is the sealed definition of an agent type
// (data-model.md §2). Identity is the Merkle root of the node's tree
// (agent-type.json + body.md [+ base-literacy.md]). Every field here is
// identity: re-tuning the model or widening mayAffect mints a new version.
type AgentTypeSnapshot struct {
	Kind            string          `json:"kind"`
	APIVersion      string          `json:"apiVersion"`
	Name            string          `json:"name"`
	Harness         string          `json:"harness"`
	Model           string          `json:"model"`
	Runtime         *AgentRuntime   `json:"runtime,omitempty"`
	AutonomyDefault string          `json:"autonomyDefault,omitempty"`
	Tools           AgentToolPolicy `json:"tools"`
	MayAffect       []string        `json:"mayAffect,omitempty"`
	Secrets         *AgentSecrets   `json:"secrets,omitempty"`
	Owner           string          `json:"owner"`
	Extends         string          `json:"extends,omitempty"` // "<literacy-name>@<objectId>"
	BodyRef         string          `json:"bodyRef"`           // persona blob id
	Catalog         string          `json:"catalog,omitempty"` // CatalogSnapshot the mayAffect globs resolved against
}

// Validate checks an AgentTypeSnapshot. BodyRef is stamped by the assembler;
// callers constructing records by hand must supply a valid blob id.
func (a AgentTypeSnapshot) Validate() error {
	if a.Kind != KindAgentTypeSnapshot {
		return invalidf("agent-type kind %q", a.Kind)
	}
	if a.APIVersion != apiVersionV1 {
		return invalidf("agent-type apiVersion %q", a.APIVersion)
	}
	if !validAgentName(a.Name) {
		return invalidf("agent-type name %q", a.Name)
	}
	if a.Harness == "" {
		return invalidf("agent-type %q harness empty", a.Name)
	}
	if a.Model == "" {
		return invalidf("agent-type %q model empty", a.Name)
	}
	if a.Owner == "" {
		// The work-plane rule adopted platform-wide: no responsible owner, no
		// seal (specs/orun-agents/data-model.md §2).
		return invalidf("agent-type %q owner empty (a responsible owner is mandatory)", a.Name)
	}
	if a.AutonomyDefault != "" && !validAutonomy(a.AutonomyDefault) {
		return invalidf("agent-type %q autonomyDefault %q", a.Name, a.AutonomyDefault)
	}
	if !validID(a.BodyRef) {
		return invalidf("agent-type %q bodyRef %q", a.Name, a.BodyRef)
	}
	if a.Extends != "" {
		name, id, ok := strings.Cut(a.Extends, "@")
		if !ok || name == "" || !validID(id) {
			return invalidf("agent-type %q extends %q (want <name>@<objectId>)", a.Name, a.Extends)
		}
	}
	if a.Catalog != "" && !validID(a.Catalog) {
		return invalidf("agent-type %q catalog %q", a.Name, a.Catalog)
	}
	return nil
}

// AgentBrief is the frozen input an agent runs from, sealed before the driver
// launches (data-model.md §3.1). Blob identity: same inputs → same brief → a
// local run and a cloud run are the same run.
type AgentBrief struct {
	Kind         string `json:"kind"`
	APIVersion   string `json:"apiVersion"`
	RunKind      string `json:"runKind"`
	Spec         string `json:"spec,omitempty"`     // SpecSnapshot id
	Task         string `json:"task,omitempty"`     // human task key (implementation/fix)
	Affected     string `json:"affected,omitempty"` // sealed AffectedSet blob id
	Literacy     string `json:"literacy,omitempty"` // base-literacy blob id
	Instructions string `json:"instructions"`       // rendered system-prompt blob id
}

// Validate checks an AgentBrief.
func (b AgentBrief) Validate() error {
	if b.Kind != KindAgentBrief {
		return invalidf("agent-brief kind %q", b.Kind)
	}
	if b.APIVersion != apiVersionV1 {
		return invalidf("agent-brief apiVersion %q", b.APIVersion)
	}
	if !validRunKind(b.RunKind) {
		return invalidf("agent-brief runKind %q", b.RunKind)
	}
	if !validID(b.Instructions) {
		return invalidf("agent-brief instructions %q", b.Instructions)
	}
	for f, v := range map[string]string{"spec": b.Spec, "affected": b.Affected, "literacy": b.Literacy} {
		if v != "" && !validID(v) {
			return invalidf("agent-brief %s %q", f, v)
		}
	}
	return nil
}

// AgentSessionEvent is one entry of the append-only session event log. It is a
// sub-record of a segment, not a top-level node.
type AgentSessionEvent struct {
	Seq     int            `json:"seq"`
	Kind    string         `json:"kind"` // closed vocabulary (ValidSessionEventKind)
	At      string         `json:"at,omitempty"`
	Ref     string         `json:"ref,omitempty"` // transcript-chunk blob id for bulk payloads
	Payload map[string]any `json:"payload,omitempty"`
}

// AgentSessionSegment is a chained slice of a session's event log
// (data-model.md §3.2), mirroring the work plane's log segments: `prev` makes
// the sealed log tamper-evident end to end. Blob identity.
type AgentSessionSegment struct {
	Kind       string              `json:"kind"`
	APIVersion string              `json:"apiVersion"`
	SessionID  string              `json:"sessionId"`
	FromSeq    int                 `json:"fromSeq"`
	ToSeq      int                 `json:"toSeq"`
	Entries    []AgentSessionEvent `json:"entries"`
	Prev       string              `json:"prev,omitempty"`
}

// Validate checks an AgentSessionSegment, including the closed event
// vocabulary and seq monotonicity within the segment window.
func (g AgentSessionSegment) Validate() error {
	if g.Kind != KindAgentSessionSegment {
		return invalidf("session-segment kind %q", g.Kind)
	}
	if g.APIVersion != apiVersionV1 {
		return invalidf("session-segment apiVersion %q", g.APIVersion)
	}
	if !strings.HasPrefix(g.SessionID, "as_") {
		return invalidf("session-segment sessionId %q lacks as_ prefix", g.SessionID)
	}
	if g.FromSeq < 0 || g.ToSeq < g.FromSeq {
		return invalidf("session-segment window [%d,%d]", g.FromSeq, g.ToSeq)
	}
	if g.Prev != "" && !validID(g.Prev) {
		return invalidf("session-segment prev %q", g.Prev)
	}
	prev := g.FromSeq - 1
	for _, e := range g.Entries {
		if !ValidSessionEventKind(e.Kind) {
			return invalidf("session-segment %s event kind %q", g.SessionID, e.Kind)
		}
		if e.Seq <= prev {
			return invalidf("session-segment %s seq %d not increasing", g.SessionID, e.Seq)
		}
		if e.Seq > g.ToSeq {
			return invalidf("session-segment %s seq %d beyond window", g.SessionID, e.Seq)
		}
		if e.Ref != "" && !validID(e.Ref) {
			return invalidf("session-segment %s event ref %q", g.SessionID, e.Ref)
		}
		prev = e.Seq
	}
	return nil
}

// AgentOutcome records how a session ended. Cost counters and wall-clock times
// are annotation, not identity, and deliberately have no fields here — they
// live in the session's cost_sample/state_changed events and on the ref.
type AgentOutcome struct {
	Status string `json:"status"` // completed|failed|canceled|expired
	PR     string `json:"pr,omitempty"`
	Branch string `json:"branch,omitempty"`
}

func validOutcomeStatus(s string) bool {
	switch s {
	case "completed", "failed", "canceled", "expired":
		return true
	default:
		return false
	}
}

// AgentSessionSnapshot is the sealed run — the system of proof for what an
// agent did, pinning every input by hash (data-model.md §3). It embeds no
// lifecycle and no gate verdicts: those live in the work plane, derived. Blob
// identity.
type AgentSessionSnapshot struct {
	Kind       string        `json:"kind"`
	APIVersion string        `json:"apiVersion"`
	SessionID  string        `json:"sessionId"`
	RunKind    string        `json:"runKind"`
	AgentType  string        `json:"agentType"` // AgentTypeSnapshot id at dispatch
	Brief      string        `json:"brief"`     // AgentBrief id
	WorkRef    string        `json:"workRef,omitempty"`
	Catalog    string        `json:"catalog,omitempty"`
	Principal  string        `json:"principal,omitempty"`
	Segments   []string      `json:"segments,omitempty"`
	Transcript string        `json:"transcript,omitempty"` // tree of transcript chunks
	Outcome    *AgentOutcome `json:"outcome,omitempty"`
}

// Validate checks an AgentSessionSnapshot.
func (s AgentSessionSnapshot) Validate() error {
	if s.Kind != KindAgentSessionSnapshot {
		return invalidf("agent-session kind %q", s.Kind)
	}
	if s.APIVersion != apiVersionV1 {
		return invalidf("agent-session apiVersion %q", s.APIVersion)
	}
	if !strings.HasPrefix(s.SessionID, "as_") {
		return invalidf("agent-session sessionId %q lacks as_ prefix", s.SessionID)
	}
	if !validRunKind(s.RunKind) {
		return invalidf("agent-session %s runKind %q", s.SessionID, s.RunKind)
	}
	if !validID(s.AgentType) {
		return invalidf("agent-session %s agentType %q", s.SessionID, s.AgentType)
	}
	if !validID(s.Brief) {
		return invalidf("agent-session %s brief %q", s.SessionID, s.Brief)
	}
	if s.Catalog != "" && !validID(s.Catalog) {
		return invalidf("agent-session %s catalog %q", s.SessionID, s.Catalog)
	}
	if s.Transcript != "" && !validID(s.Transcript) {
		return invalidf("agent-session %s transcript %q", s.SessionID, s.Transcript)
	}
	for _, seg := range s.Segments {
		if !validID(seg) {
			return invalidf("agent-session %s segment %q", s.SessionID, seg)
		}
	}
	if s.Outcome != nil && !validOutcomeStatus(s.Outcome.Status) {
		return invalidf("agent-session %s outcome status %q", s.SessionID, s.Outcome.Status)
	}
	return nil
}

// AssembleAgentType writes the persona body (and optional literacy body) as
// content-addressed blobs, stamps BodyRef/Extends, writes the record, and
// bundles all of it into the node's tree. Identity = the tree's Merkle root,
// so a persona edit mints a new version while byte-identical personas dedup —
// the doc-blob spine (data-model.md §2.1).
//
// literacyName/literacyBody are optional; when supplied the literacy blob
// joins the tree and Extends is stamped "<literacyName>@<blobId>". When
// absent, any caller-supplied Extends pin is kept as-is.
func AssembleAgentType(ctx context.Context, s store, at AgentTypeSnapshot, body []byte, literacyName string, literacyBody []byte) (ObjectID, error) {
	if len(body) == 0 {
		return "", invalidf("agent-type %q persona body empty", at.Name)
	}
	bodyID, err := s.PutBlob(ctx, body)
	if err != nil {
		return "", err
	}
	at.Kind = KindAgentTypeSnapshot
	at.APIVersion = apiVersionV1
	at.BodyRef = string(bodyID)

	var literacyID ObjectID
	if len(literacyBody) > 0 {
		if literacyName == "" {
			return "", invalidf("agent-type %q literacy body without a name", at.Name)
		}
		literacyID, err = s.PutBlob(ctx, literacyBody)
		if err != nil {
			return "", err
		}
		at.Extends = literacyName + "@" + string(literacyID)
	}

	if err := at.Validate(); err != nil {
		return "", err
	}
	rec, err := Encode(at)
	if err != nil {
		return "", err
	}
	recID, err := s.PutBlob(ctx, rec)
	if err != nil {
		return "", err
	}

	entries := []objectstore.TreeEntry{
		blobEntry(fileAgentType, recID),
		blobEntry(fileAgentBody, bodyID),
	}
	if literacyID != "" {
		entries = append(entries, blobEntry(fileAgentLiteracy, literacyID))
	}
	return s.PutTree(ctx, entries)
}

// AssembleAgentBrief writes the brief record as a blob and returns its id.
func AssembleAgentBrief(ctx context.Context, s store, b AgentBrief) (ObjectID, error) {
	b.Kind = KindAgentBrief
	b.APIVersion = apiVersionV1
	if err := b.Validate(); err != nil {
		return "", err
	}
	rec, err := Encode(b)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, rec)
}

// AssembleAgentSessionSegment writes one chained slice of a session's event
// log as a blob and returns its id.
func AssembleAgentSessionSegment(ctx context.Context, s store, g AgentSessionSegment) (ObjectID, error) {
	g.Kind = KindAgentSessionSegment
	g.APIVersion = apiVersionV1
	if err := g.Validate(); err != nil {
		return "", err
	}
	rec, err := Encode(g)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, rec)
}

// AssembleAgentSession writes the sealed session record as a blob and returns
// its id.
func AssembleAgentSession(ctx context.Context, s store, sn AgentSessionSnapshot) (ObjectID, error) {
	sn.Kind = KindAgentSessionSnapshot
	sn.APIVersion = apiVersionV1
	if err := sn.Validate(); err != nil {
		return "", err
	}
	rec, err := Encode(sn)
	if err != nil {
		return "", err
	}
	return s.PutBlob(ctx, rec)
}
