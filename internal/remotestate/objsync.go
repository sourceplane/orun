package remotestate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// objectKindHeader names the content-addressed object kind on object PUTs
// (plan | catalog-snapshot | composition-lock | artifact-manifest). The server
// requires it (state-api-contract §3).
const objectKindHeader = "Orun-Object-Kind"

// ObjectKindPlan is the object kind for a serialized plan blob.
const ObjectKindPlan = "plan"

// ObjectsMissingRequest is the body for POST …/state/objects/missing.
type ObjectsMissingRequest struct {
	Digests []string `json:"digests"`
}

// ObjectsMissingResponse is returned by the digest-negotiation endpoint.
type ObjectsMissingResponse struct {
	Missing []string `json:"missing"`
}

// digestSegment renders a digest for an object path. A digest is
// "sha256:<hex>"; the colon is a legal URL path-segment character (RFC 3986)
// and the server matches the raw form, so it must NOT be percent-encoded — the
// generic urlSegment escapes ":" to "%3A", which the state-worker router does
// not match (it 404s "Route not found"). Only a stray "/" would need escaping,
// which a valid digest never contains; we escape it defensively.
func digestSegment(digest string) string {
	return strings.ReplaceAll(digest, "/", "%2F")
}

// Digest computes the content address ("sha256:<hex>") of a blob, matching the
// platform's server-side verification.
func Digest(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ObjectsMissing asks the object plane which of the given digests are absent,
// so the client only uploads the gaps (state-api-contract §3 digest
// negotiation).
func (c *Client) ObjectsMissing(ctx context.Context, digests []string) ([]string, error) {
	var resp ObjectsMissingResponse
	if err := c.doJSON(ctx, http.MethodPost, c.statePath("/objects/missing"),
		ObjectsMissingRequest{Digests: digests}, &resp, true); err != nil {
		return nil, fmt.Errorf("objects missing: %w", err)
	}
	return resp.Missing, nil
}

// PutObject uploads a single object blob (≤ the 25 MiB single-request budget)
// by its digest. The PUT is idempotent: a re-push of an existing digest is a
// server no-op. Blobs larger than the single-request budget require the
// chunked-upload sub-protocol (OC4); this client uploads only small blobs
// (plans) for now and surfaces the platform's payload_too_large otherwise.
func (c *Client) PutObject(ctx context.Context, digest, kind string, blob []byte) error {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolving auth token: %w", err)
	}
	path := c.statePath("/objects/" + digestSegment(digest))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(blob))
	if err != nil {
		return fmt.Errorf("building object put request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(contractVersionHeader, ContractVersion)
	req.Header.Set(objectKindHeader, kind)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(blob))

	resp, err := c.logClient.Do(req)
	if err != nil {
		return fmt.Errorf("object put request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return nil
}

// GetObject downloads an object blob by digest. Returns (nil, nil) when the
// object does not exist.
func (c *Client) GetObject(ctx context.Context, digest string) ([]byte, error) {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving auth token: %w", err)
	}
	path := c.statePath("/objects/" + digestSegment(digest))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building object get request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(contractVersionHeader, ContractVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("object get request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, c.parseError(resp)
	}
	return io.ReadAll(resp.Body)
}

// EnsureObject uploads blob only if its digest is missing from the object plane
// (digest negotiation first, then a gap-filling PUT). Returns the digest so the
// caller can reference it (e.g. as a run's planDigest). Idempotent and safe to
// call on every run.
func (c *Client) EnsureObject(ctx context.Context, kind string, blob []byte) (string, error) {
	digest := Digest(blob)
	missing, err := c.ObjectsMissing(ctx, []string{digest})
	if err != nil {
		return "", err
	}
	present := true
	for _, d := range missing {
		if d == digest {
			present = false
			break
		}
	}
	if present {
		return digest, nil
	}
	if err := c.PutObject(ctx, digest, kind, blob); err != nil {
		return "", err
	}
	return digest, nil
}

// PlanBlob serializes a backend plan to its canonical JSON object representation
// and returns (blob, digest). The same bytes are uploaded to the object plane
// and referenced by the run's planDigest, so the digest the server verifies
// always matches what was stored.
func PlanBlob(bp *BackendPlan) ([]byte, string, error) {
	blob, err := json.Marshal(bp)
	if err != nil {
		return nil, "", fmt.Errorf("serializing plan: %w", err)
	}
	return blob, Digest(blob), nil
}
