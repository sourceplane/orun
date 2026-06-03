package main

// read_resolve.go holds openLocalStateStore, the shared helper that opens the
// workspace's local statestore (.orun/) for the catalog read commands. The M5.c
// legacy execution resolver that used to live here was removed at the M12
// cutover: execution reads now go through the content-addressed object graph
// (internal/objread).

import (
	"fmt"
	"path/filepath"

	"github.com/sourceplane/orun/internal/statestore"
)

// openLocalStateStore opens the workspace's `.orun/` local state store and
// returns it plus the absolute path used.
func openLocalStateStore() (statestore.StateStore, string, error) {
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, "", fmt.Errorf("resolve store root: %w", err)
	}
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: abs})
	if err != nil {
		return nil, abs, fmt.Errorf("open state store: %w", err)
	}
	return store, abs, nil
}
