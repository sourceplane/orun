package statestore

import "errors"

// ErrNotFound indicates a Read / CompareAndSwap target did not exist. Returned
// errors wrap this sentinel via fmt.Errorf("%w: …", ErrNotFound, …); callers
// MUST detect via errors.Is rather than string sniffing.
var ErrNotFound = errors.New("statestore: object not found")

// ErrExists indicates a CreateIfAbsent collided with an existing object.
// Returned errors wrap this sentinel via fmt.Errorf("%w: …", ErrExists, …);
// callers MUST detect via errors.Is.
var ErrExists = errors.New("statestore: object already exists")

// ErrConflict indicates a CompareAndSwap observed a Revision mismatch — i.e.
// the object exists but its current revision does not equal the caller's
// oldRev. Returned errors wrap this sentinel; detect via errors.Is.
var ErrConflict = errors.New("statestore: compare-and-swap conflict")

// ErrInvalid indicates an invalid argument: a path that violates the path
// policy (see paths.go), an attempt to Delete a non-empty directory, or a
// similar caller mistake. Returned errors wrap this sentinel via fmt.Errorf;
// detect via errors.Is.
var ErrInvalid = errors.New("statestore: invalid path or argument")
