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
	"strconv"
	"strings"
)

// singleShotMaxBytes is the platform's default single-request body budget
// (state-api-contract §3): blobs at or under it PUT in one shot, larger ones use
// the chunked multipart sub-protocol. A var (not const) so tests can lower it
// to exercise the multipart path without allocating a 25 MiB blob.
var singleShotMaxBytes = 25 * 1024 * 1024

// defaultPartSize is the fallback chunk size when a start-upload response omits
// partSize.
const defaultPartSize = 8 * 1024 * 1024

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
// server no-op. Larger blobs go through putObjectMultipart; EnsureObject picks
// the path by size, so callers normally use EnsureObject rather than this
// directly.
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
	// Single-request budget vs the chunked multipart sub-protocol (contract §3).
	if len(blob) <= singleShotMaxBytes {
		if err := c.PutObject(ctx, digest, kind, blob); err != nil {
			return "", err
		}
	} else if err := c.putObjectMultipart(ctx, digest, kind, blob); err != nil {
		return "", err
	}
	return digest, nil
}

// authedObjectRequest builds an authenticated object-plane request carrying the
// standard headers (auth, user-agent, contract version) plus any extras.
func (c *Client) authedObjectRequest(ctx context.Context, method, path string, body io.Reader, extra map[string]string) (*http.Request, error) {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving auth token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building object request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(contractVersionHeader, ContractVersion)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return req, nil
}

// putObjectMultipart uploads a blob too large for the single-request budget via
// the chunked sub-protocol (state-api-contract §3): start → PUT each part →
// complete. The server reassembles from its own per-part records and verifies
// the assembled digest, so the client sends no part list at complete-time — only
// the object kind header.
func (c *Client) putObjectMultipart(ctx context.Context, digest, kind string, blob []byte) error {
	uploadID, partSize, err := c.startUpload(ctx, digest)
	if err != nil {
		return err
	}
	if partSize <= 0 {
		partSize = defaultPartSize
	}
	partNumber := 1
	for off := 0; off < len(blob); off += partSize {
		end := off + partSize
		if end > len(blob) {
			end = len(blob)
		}
		if err := c.uploadPart(ctx, digest, uploadID, partNumber, blob[off:end]); err != nil {
			return fmt.Errorf("multipart part %d: %w", partNumber, err)
		}
		partNumber++
	}
	if err := c.completeUpload(ctx, digest, uploadID, kind); err != nil {
		return fmt.Errorf("multipart complete: %w", err)
	}
	return nil
}

// startUpload opens a chunked upload and returns the server's uploadId and the
// part size the client must chunk to.
func (c *Client) startUpload(ctx context.Context, digest string) (uploadID string, partSize int, err error) {
	req, err := c.authedObjectRequest(ctx, http.MethodPost,
		c.statePath("/objects/"+digestSegment(digest)+"/uploads"), nil, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("start upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", 0, c.parseError(resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("reading start upload response: %w", err)
	}
	var out struct {
		UploadID string `json:"uploadId"`
		PartSize int    `json:"partSize"`
	}
	if err := decodeSuccessBody(body, &out); err != nil {
		return "", 0, fmt.Errorf("decoding start upload response: %w", err)
	}
	if out.UploadID == "" {
		return "", 0, fmt.Errorf("start upload: server returned no uploadId")
	}
	return out.UploadID, out.PartSize, nil
}

// uploadPart PUTs one part's bytes. The uploadId is a server-generated R2
// multipart id (path-segment safe; never contains '/').
func (c *Client) uploadPart(ctx context.Context, digest, uploadID string, partNumber int, part []byte) error {
	path := c.statePath("/objects/" + digestSegment(digest) + "/uploads/" + uploadID + "/parts/" + strconv.Itoa(partNumber))
	req, err := c.authedObjectRequest(ctx, http.MethodPut, path, bytes.NewReader(part),
		map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(part))
	resp, err := c.logClient.Do(req) // longer timeout for large parts
	if err != nil {
		return fmt.Errorf("upload part request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return nil
}

// completeUpload finalizes the chunked upload; the object kind travels in the
// Orun-Object-Kind header (the server applies it to the assembled object).
func (c *Client) completeUpload(ctx context.Context, digest, uploadID, kind string) error {
	path := c.statePath("/objects/" + digestSegment(digest) + "/uploads/" + uploadID + "/complete")
	req, err := c.authedObjectRequest(ctx, http.MethodPost, path, nil,
		map[string]string{objectKindHeader: kind})
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("complete upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return nil
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
