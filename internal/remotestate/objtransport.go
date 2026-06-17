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
	_ objectstore.ObjectTransport = objectTransport{}
	_ refstore.RefTransport       = refTransport{}
)
