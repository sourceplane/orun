package nodes

import (
	"context"

	"github.com/sourceplane/orun/internal/objectstore"
)

// hashStore is a store implementation that computes object ids without
// persisting anything. Because the assemblers depend only on PutBlob/PutTree,
// running an assembler against a hashStore yields the node's content id with no
// I/O — so the pure id helpers below reuse the exact assembly logic and can
// never drift from what the real assemblers write.
type hashStore struct{ algo objectstore.Algo }

func (h hashStore) PutBlob(_ context.Context, data []byte) (objectstore.ObjectID, error) {
	return objectstore.HashBlob(h.algo, data)
}

func (h hashStore) PutTree(_ context.Context, entries []objectstore.TreeEntry) (objectstore.ObjectID, error) {
	return objectstore.HashTree(h.algo, entries)
}

// SourceID returns the content id AssembleSource would produce for src.
func SourceID(algo objectstore.Algo, src SourceSnapshot) (ObjectID, error) {
	return AssembleSource(context.Background(), hashStore{algo}, src)
}

// CatalogID returns the content id AssembleCatalog would produce.
func CatalogID(algo objectstore.Algo, cat CatalogSnapshot, manifests []ComponentManifest, graphs []CatalogGraph, ownership ImpactOwnership, fingerprints []ComponentFingerprint) (ObjectID, error) {
	return AssembleCatalog(context.Background(), hashStore{algo}, cat, manifests, graphs, ownership, fingerprints)
}

// RevisionID returns the content id AssembleRevision would produce for
// (rev, planBytes), without writing. Used for Has-gated reuse detection: two
// triggers compiling an identical plan derive the same RevisionID.
func RevisionID(algo objectstore.Algo, rev PlanRevision, planBytes []byte) (ObjectID, error) {
	return AssembleRevision(context.Background(), hashStore{algo}, rev, planBytes)
}
