package remotestate

import (
	"context"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// objectTransport adapts the state API object plane (digest-keyed CAS over R2)
// to objectstore.ObjectTransport, so an objectstore.RemoteStore drives the
// hosted ObjectStore exactly like a local one. The uploaded/downloaded bytes are
// the object model's framed serialization (digest = hash of the framed bytes),
// so an id is identical local and remote — dedup is global.
type objectTransport struct{ c *Client }

// HasObject reports presence via digest negotiation (a digest absent from the
// missing set is present).
func (t objectTransport) HasObject(ctx context.Context, digest string) (bool, error) {
	missing, err := t.c.ObjectsMissing(ctx, []string{digest})
	if err != nil {
		return false, err
	}
	for _, d := range missing {
		if d == digest {
			return false, nil
		}
	}
	return true, nil
}

// objectsMissingChunk bounds how many digests ride in one /objects/missing
// negotiation. A closure larger than this is split across requests so the JSON
// body stays well under the platform's request budget; in practice a catalog
// closure is a single chunk, so a push negotiates presence in one round-trip
// instead of one per object.
const objectsMissingChunk = 1000

// MissingObjects reports which of digests the object plane lacks, batching the
// digest negotiation (chunked) rather than probing one digest at a time. This is
// the capability objectstore.RemoteStore.MissingObjects / objremote.Sync use to
// collapse a push's set-difference scan from O(closure) round-trips to ~1.
// Implements objectstore.BatchMissingTransport.
func (t objectTransport) MissingObjects(ctx context.Context, digests []string) ([]string, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	var missing []string
	for start := 0; start < len(digests); start += objectsMissingChunk {
		end := start + objectsMissingChunk
		if end > len(digests) {
			end = len(digests)
		}
		got, err := t.c.ObjectsMissing(ctx, digests[start:end])
		if err != nil {
			return nil, err
		}
		missing = append(missing, got...)
	}
	return missing, nil
}

// GetObject downloads the framed bytes; a 404 maps to ok=false.
func (t objectTransport) GetObject(ctx context.Context, digest string) ([]byte, bool, error) {
	body, err := t.c.GetObject(ctx, digest)
	if err != nil {
		return nil, false, err
	}
	if body == nil {
		return nil, false, nil
	}
	return body, true, nil
}

// PutObject stores framed bytes, routing oversize blobs through the chunked
// multipart sub-protocol. Idempotent server-side.
func (t objectTransport) PutObject(ctx context.Context, digest, kind string, framed []byte) error {
	if len(framed) <= singleShotMaxBytes {
		return t.c.PutObject(ctx, digest, kind, framed)
	}
	return t.c.putObjectMultipart(ctx, digest, kind, framed)
}

// refTransport adapts the state API ref plane (CAS pointers) to
// refstore.RefTransport.
type refTransport struct{ c *Client }

func (t refTransport) ReadRef(ctx context.Context, name string) (refstore.Ref, bool, error) {
	rec, ok, err := t.c.GetRef(ctx, name)
	if err != nil || !ok {
		return refstore.Ref{}, false, err
	}
	return rec.toRef(), true, nil
}

func (t refTransport) UpdateRef(ctx context.Context, name, oldTarget, newTarget string) error {
	return t.c.UpdateRef(ctx, name, oldTarget, newTarget)
}

func (t refTransport) ListRefs(ctx context.Context, prefix string) ([]string, error) {
	return t.c.ListRefs(ctx, prefix)
}

func (t refTransport) DeleteRef(ctx context.Context, name string) error {
	return t.c.DeleteRef(ctx, name)
}

// RemoteStores builds a hosted (ObjectStore, RefStore) pair over this client —
// the seam objmodel.NewReader / bridge.FromRemoteModel consume so the console
// and `orun tui --remote` read source → catalog → execution history off the
// SAME object-model readers the local TUI uses. The store addresses objects
// under the default algorithm (sha256), matching local digests.
func (c *Client) RemoteStores() (objectstore.ObjectStore, refstore.RefStore) {
	store := objectstore.NewRemoteStore(objectTransport{c: c}, objectstore.DefaultAlgo, c.baseURL)
	refs := refstore.NewRemoteRefStore(refTransport{c: c})
	return store, refs
}

// Interface conformance — these adapt the objectstore/refstore transport seams.
var (
	_ objectstore.ObjectTransport       = objectTransport{}
	_ objectstore.BatchMissingTransport = objectTransport{}
	_ refstore.RefTransport             = refTransport{}
)
