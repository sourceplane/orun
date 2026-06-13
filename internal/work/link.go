package work

import (
	"errors"
	"fmt"
)

// LinkType is a relation-edge type in the work vocabulary (data-model.md §7).
// It shares the catalog graph's grammar so the affected engine can traverse
// work and catalog edges uniformly once SC2 unifies the stores.
type LinkType string

// The closed set of work relation-edge types (data-model.md §7).
const (
	LinkPartOf        LinkType = "partOf"
	LinkHasPart       LinkType = "hasPart"
	LinkAffects       LinkType = "affects"
	LinkBlockedBy     LinkType = "blockedBy"
	LinkBlocks        LinkType = "blocks"
	LinkImplementedBy LinkType = "implementedBy"
	LinkDelivers      LinkType = "delivers"
	LinkAssignedTo    LinkType = "assignedTo"
)

// LinkTypes is the closed set of valid edge types.
var LinkTypes = map[LinkType]bool{
	LinkPartOf:        true,
	LinkHasPart:       true,
	LinkAffects:       true,
	LinkBlockedBy:     true,
	LinkBlocks:        true,
	LinkImplementedBy: true,
	LinkDelivers:      true,
	LinkAssignedTo:    true,
}

// Link is a typed relation edge between work entities (or between a work entity
// and a catalog component / revision / deployment), stored in work_links until
// SC2 unifies the graphs (data-model.md §7).
type Link struct {
	Project   string   `json:"project"`
	From      string   `json:"from"`
	FromKind  string   `json:"fromKind"`
	Type      LinkType `json:"type"`
	To        string   `json:"to"`
	ToKind    string   `json:"toKind"`
	CreatedBy Actor    `json:"createdBy"`
	CreatedAt string   `json:"createdAt"`
}

// ErrInvalidLink is returned by Link.Validate on a malformed edge.
var ErrInvalidLink = errors.New("work: invalid link")

// Validate checks the edge has a known type and non-empty endpoints.
func (l Link) Validate() error {
	if !LinkTypes[l.Type] {
		return fmt.Errorf("%w: type %q is not in the work vocabulary", ErrInvalidLink, l.Type)
	}
	if l.Project == "" || l.From == "" || l.To == "" {
		return fmt.Errorf("%w: project, from and to are required", ErrInvalidLink)
	}
	return nil
}

// identity is the (project, from, type, to) primary key of work_links — the
// dedupe identity the projection uses.
func (l Link) identity() [4]string {
	return [4]string{l.Project, l.From, string(l.Type), l.To}
}
