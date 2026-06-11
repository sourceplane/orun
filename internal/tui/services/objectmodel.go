package services

import (
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// openObjectModel opens the content-addressed object store + ref store under
// <objectModelRoot>/objectmodel. ok=false when the root is unset, the store
// has no content yet (an empty workspace), or either store fails to open —
// every cockpit read treats all three as "no object model" and degrades
// rather than erroring.
func openObjectModel(objectModelRoot string) (store *objectstore.LocalStore, refs *refstore.LocalRefStore, ok bool) {
	if objectModelRoot == "" {
		return nil, nil, false
	}
	root := filepath.Join(objectModelRoot, "objectmodel")
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, nil, false
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, nil, false
	}
	refs, err = refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui"})
	if err != nil {
		return nil, nil, false
	}
	return store, refs, true
}
