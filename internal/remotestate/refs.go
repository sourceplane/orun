package remotestate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// RefRecord is the wire projection of a hosted ref (state-api §refs, OV1):
// a mutable name → ObjectID pointer.
type RefRecord struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Writer    string `json:"writer"`
	UpdatedAt string `json:"updatedAt"`
}

type getRefResponse struct {
	Ref RefRecord `json:"ref"`
}

type updateRefResponse struct {
	Ref RefRecord `json:"ref"`
}

type listRefsResponse struct {
	Refs []RefRecord `json:"refs"`
}

// updateRefRequest is the PUT body: compare-and-swap from expectedTarget
// ("" = require absent) to target.
type updateRefRequest struct {
	ExpectedTarget string `json:"expectedTarget"`
	Target         string `json:"target"`
}

// refPath builds a scoped ref path. The ref name carries literal slashes
// (catalogs/current, executions/by-id/<id>) that the server matches as a greedy
// tail, so they are NOT percent-encoded; the ref-name alphabet ([A-Za-z0-9._-/])
// needs no escaping.
func (c *Client) refPath(name string) string {
	return c.statePath("/refs/" + name)
}

// GetRef resolves a ref by name, returning (record, found, error). A 404 maps to
// found=false rather than an error so callers can distinguish "absent" cleanly.
func (c *Client) GetRef(ctx context.Context, name string) (RefRecord, bool, error) {
	var resp getRefResponse
	err := c.doJSON(ctx, http.MethodGet, c.refPath(name), nil, &resp, true)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusNotFound {
			return RefRecord{}, false, nil
		}
		return RefRecord{}, false, fmt.Errorf("get ref %q: %w", name, err)
	}
	return resp.Ref, true, nil
}

// UpdateRef compare-and-swaps a ref. A 409 ref_conflict maps to
// refstore.ErrConflict so the sync engine's bounded CAS retry works unchanged; a
// 412 object_missing surfaces as a descriptive error (the closure wasn't
// uploaded). Not retried on 5xx: a CAS is not safe to replay blindly.
func (c *Client) UpdateRef(ctx context.Context, name, expectedTarget, newTarget string) error {
	req := updateRefRequest{ExpectedTarget: expectedTarget, Target: newTarget}
	var resp updateRefResponse
	err := c.doJSON(ctx, http.MethodPut, c.refPath(name), req, &resp, false)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok {
			if strings.EqualFold(apiErr.Code, "ref_conflict") {
				return refstore.ErrConflict
			}
		}
		return fmt.Errorf("update ref %q: %w", name, err)
	}
	return nil
}

// ListRefs lists ref names under a prefix.
func (c *Client) ListRefs(ctx context.Context, prefix string) ([]string, error) {
	path := c.statePath("/refs")
	if prefix != "" {
		path += "?prefix=" + url.QueryEscape(prefix)
	}
	var resp listRefsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, fmt.Errorf("list refs %q: %w", prefix, err)
	}
	names := make([]string, 0, len(resp.Refs))
	for _, r := range resp.Refs {
		names = append(names, r.Name)
	}
	return names, nil
}

// DeleteRef removes a ref by name (idempotent server-side).
func (c *Client) DeleteRef(ctx context.Context, name string) error {
	if err := c.doJSON(ctx, http.MethodDelete, c.refPath(name), nil, nil, true); err != nil {
		return fmt.Errorf("delete ref %q: %w", name, err)
	}
	return nil
}

// toRef converts a wire RefRecord into the object-model refstore.Ref the
// RemoteRefStore returns.
func (r RefRecord) toRef() refstore.Ref {
	ref := refstore.Ref{Kind: "Ref", Target: r.Target, Writer: r.Writer}
	if r.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, r.UpdatedAt); err == nil {
			ref.UpdatedAt = t
		}
	}
	return ref
}
