package objectstore

// HashBlob computes the content id a blob of data would have under algo, without
// storing anything. It is the pure counterpart of PutBlob — used by higher
// layers (nodewriter, indexes, remote) to derive an object id for a Has-gated
// reuse check before deciding whether to write.
func HashBlob(algo Algo, data []byte) (ObjectID, error) {
	_, id, err := computeBlobID(algo, data)
	return id, err
}

// HashTree computes the content id a tree of entries would have under algo,
// without storing anything. Entries are validated and sorted exactly as PutTree
// does, so the returned id matches a subsequent PutTree of the same entries.
func HashTree(algo Algo, entries []TreeEntry) (ObjectID, error) {
	_, _, id, err := computeTree(algo, entries)
	return id, err
}
