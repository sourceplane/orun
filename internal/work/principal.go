package work

import (
	"errors"
	"fmt"
)

// ActorType is the kind of principal an event is attributed to (data-model.md
// §4). It is deliberately distinct from PrincipalType: a stored principal is a
// human or an agent, but an *event* may also be attributed to automation (a
// named rule, e.g. the PR auto-linker), which is not a standing principal.
type ActorType string

// The closed set of event actor types (data-model.md §4).
const (
	ActorUser       ActorType = "user"
	ActorAgent      ActorType = "agent"
	ActorAutomation ActorType = "automation"
)

var actorTypes = map[ActorType]bool{
	ActorUser:       true,
	ActorAgent:      true,
	ActorAutomation: true,
}

// Actor names who caused an event (data-model.md §4). It doubles as the entity
// envelope's createdBy reference, where Via is omitted.
type Actor struct {
	// Type is user | agent | automation.
	Type ActorType `json:"type"`
	// ID is a principal id (user/agent) or an automation rule id
	// (e.g. "bridge/pr-linker").
	ID string `json:"id"`
	// Via records the surface the mutation came through: mcp | ui | cli |
	// webhook | github-webhook | import. Optional.
	Via string `json:"via,omitempty"`
}

// ErrMissingActor is returned when an event or mutation carries no valid actor.
// Every event MUST name one (invariant 4, W0 "an event without an actor is
// rejected").
var ErrMissingActor = errors.New("work: event has no actor")

// Validate checks the actor has a known type and a non-empty id.
func (a Actor) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("%w: actor id is empty", ErrMissingActor)
	}
	if !actorTypes[a.Type] {
		return fmt.Errorf("%w: actor type %q is not one of user|agent|automation", ErrMissingActor, a.Type)
	}
	return nil
}

// IsHuman reports whether the actor is a human user. Automation must never be
// attributed to a human (invariant 4); callers building automation events use
// ActorAutomation.
func (a Actor) IsHuman() bool { return a.Type == ActorUser }

// PrincipalType is the kind of a standing principal (data-model.md §6).
type PrincipalType string

// The closed set of principal types.
const (
	PrincipalHuman PrincipalType = "human"
	PrincipalAgent PrincipalType = "agent"
)

// GitHubIdentity is the portable GitHub identity a human principal maps onto
// (data-model.md §6; the userId is the portable id per orun-secrets SD-4).
type GitHubIdentity struct {
	UserID int64 `json:"userId"`
}

// Principal is a human or agent, modeled uniformly (WD-10). Until
// orun-service-catalog lands its User/Group kinds, principals live in D1 and map
// onto the backend's existing GitHub identities.
type Principal struct {
	ID          string          `json:"id"`
	Type        PrincipalType   `json:"type"`
	Handle      string          `json:"handle"`
	DisplayName string          `json:"displayName,omitempty"`
	GitHub      *GitHubIdentity `json:"github,omitempty"`
	// Owner is the responsible human/team principal id. Agents MUST name one
	// (WD-10); a human principal leaves it empty.
	Owner string `json:"owner,omitempty"`
}

// ErrInvalidPrincipal is returned by Principal.Validate on a malformed
// principal.
var ErrInvalidPrincipal = errors.New("work: invalid principal")

// Validate checks the principal's required fields and the agent-ownership rule.
func (p Principal) Validate() error {
	if p.ID == "" || p.Handle == "" {
		return fmt.Errorf("%w: id and handle are required", ErrInvalidPrincipal)
	}
	switch p.Type {
	case PrincipalHuman, PrincipalAgent:
	default:
		return fmt.Errorf("%w: type %q must be human|agent", ErrInvalidPrincipal, p.Type)
	}
	if p.Type == PrincipalAgent && p.Owner == "" {
		return fmt.Errorf("%w: agent %q must name a responsible owner", ErrInvalidPrincipal, p.Handle)
	}
	return nil
}

// ActorType maps a stored principal onto the event actor type it writes as.
func (p Principal) ActorType() ActorType {
	if p.Type == PrincipalAgent {
		return ActorAgent
	}
	return ActorUser
}
