package objplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/objectstore"
)

// ResolveMemo is the input-addressed resolve cache (identity-and-keys.md §7): it
// maps (sourceId, resolverVersion) to the catalogId that resolution produced,
// so a re-plan on an unchanged source skips the catalog resolver pipeline. The
// cache is derived state under <root>/cache/resolve/ — deletable and
// rebuildable; a miss only costs a recompute, never correctness.
type ResolveMemo struct {
	dir string
}

// NewResolveMemo returns a memo rooted at <root>/cache/resolve.
func NewResolveMemo(root string) *ResolveMemo {
	return &ResolveMemo{dir: filepath.Join(root, "cache", "resolve")}
}

type memoEntry struct {
	CatalogID string `json:"catalogId"`
}

// key derives the cache filename for a source id + resolver version. The source
// id's hex tail is used (the "<algo>:" prefix is folded out) to keep the name in
// the path alphabet.
func (m *ResolveMemo) key(sourceID objectstore.ObjectID, resolverVersion int) string {
	hex := string(sourceID)
	if i := strings.IndexByte(hex, ':'); i >= 0 {
		hex = hex[i+1:]
	}
	return filepath.Join(m.dir, hex+"-rv"+strconv.Itoa(resolverVersion)+".json")
}

// Get returns the memoized catalog id for (sourceID, resolverVersion), or
// ok=false on a miss.
func (m *ResolveMemo) Get(sourceID objectstore.ObjectID, resolverVersion int) (objectstore.ObjectID, bool) {
	data, err := os.ReadFile(m.key(sourceID, resolverVersion))
	if err != nil {
		return "", false
	}
	var e memoEntry
	if err := json.Unmarshal(data, &e); err != nil || e.CatalogID == "" {
		return "", false
	}
	return objectstore.ObjectID(e.CatalogID), true
}

// Put records the catalog id for (sourceID, resolverVersion). Best-effort and
// atomic (temp + rename); a write failure is returned but is non-fatal to the
// caller (the cache is derived).
func (m *ResolveMemo) Put(sourceID objectstore.ObjectID, resolverVersion int, catalogID objectstore.ObjectID) error {
	if err := objectstore.ValidateID(catalogID); err != nil {
		return fmt.Errorf("memo: %w", err)
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("memo: mkdir: %w", err)
	}
	data, err := json.Marshal(memoEntry{CatalogID: string(catalogID)})
	if err != nil {
		return fmt.Errorf("memo: marshal: %w", err)
	}
	path := m.key(sourceID, resolverVersion)
	tmp, err := os.CreateTemp(m.dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("memo: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("memo: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("memo: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("memo: rename: %w", err)
	}
	return nil
}
