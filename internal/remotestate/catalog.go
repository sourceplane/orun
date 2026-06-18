package remotestate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// CatalogHeadRecord is the wire projection of a catalog head (state-api §catalog,
// OP3): the immutable per-(project, environment) pointer to a catalog snapshot
// digest, with the source commit it was resolved at.
type CatalogHeadRecord struct {
	OrgID       string `json:"orgId"`
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	Digest      string `json:"digest"`
	Commit      string `json:"commit"`
	AdvancedAt  string `json:"advancedAt"`
}

// advanceCatalogHeadRequest is the PUT body for /state/catalog/head. environment
// is omitted for the project-wide head; commit carries the source provenance.
type advanceCatalogHeadRequest struct {
	Digest      string `json:"digest"`
	Environment string `json:"environment,omitempty"`
	Commit      string `json:"commit,omitempty"`
}

type advanceCatalogHeadResponse struct {
	Head     CatalogHeadRecord  `json:"head"`
	Previous *CatalogHeadRecord `json:"previous"`
}

// AdvanceCatalogHead advances the (project, environment) catalog head to digest,
// recording the source git commit. environment "" targets the project-wide head.
// Returns the new head and the head it replaced (nil on the first advance).
//
// A 412 object_missing surfaces as a descriptive error (the snapshot closure was
// not uploaded first — `catalog push` must EnsureObject the closure before
// advancing). Not retried: an advance is a deliberate state transition.
func (c *Client) AdvanceCatalogHead(ctx context.Context, digest, environment, commit string) (CatalogHeadRecord, *CatalogHeadRecord, error) {
	req := advanceCatalogHeadRequest{
		Digest:      digest,
		Environment: strings.TrimSpace(environment),
		Commit:      strings.TrimSpace(commit),
	}
	var resp advanceCatalogHeadResponse
	err := c.doJSON(ctx, http.MethodPut, c.statePath("/catalog/head"), req, &resp, false)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && strings.EqualFold(apiErr.Code, "object_missing") {
			return CatalogHeadRecord{}, nil, fmt.Errorf("advance catalog head: snapshot %s is not uploaded; push its objects first: %w", digest, err)
		}
		return CatalogHeadRecord{}, nil, fmt.Errorf("advance catalog head to %s: %w", digest, err)
	}
	return resp.Head, resp.Previous, nil
}
