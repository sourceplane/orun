package objectstore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// LocalStore is the filesystem ObjectStore driver: loose, zstd-compressed
// objects at objects/<algo>/<hex[:2]>/<hex[2:]> under a root. Writes are atomic
// (temp file + fsync + rename) and idempotent. This is the v1 production driver;
// the interface is packfile-ready so a later packed driver is transparent.
type LocalStore struct {
	root string
	algo Algo
	enc  *zstd.Encoder
	dec  *zstd.Decoder
}

// LocalConfig configures a LocalStore.
type LocalConfig struct {
	// Root is the store root. The object tree lives at <Root>/objects/.
	Root string
	// Algo is the hash algorithm; DefaultAlgo when empty.
	Algo Algo
	// ZstdLevel overrides the compression level; when zero the value of
	// ORUN_OBJECT_ZSTD_LEVEL is used, defaulting to 3 (fast). Level never
	// affects object identity.
	ZstdLevel int
}

// NewLocalStore constructs a LocalStore, creating the objects/ root if needed.
func NewLocalStore(cfg LocalConfig) (*LocalStore, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("%w: empty local store root", ErrInvalid)
	}
	algo := cfg.Algo
	if algo == "" {
		algo = DefaultAlgo
	}
	if _, err := algo.hexLen(); err != nil {
		return nil, err
	}
	level := cfg.ZstdLevel
	if level == 0 {
		level = zstdLevelFromEnv()
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	if err != nil {
		return nil, fmt.Errorf("%w: zstd encoder: %v", ErrInvalid, err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: zstd decoder: %v", ErrInvalid, err)
	}
	objRoot := filepath.Join(cfg.Root, "objects", string(algo))
	if err := os.MkdirAll(objRoot, 0o755); err != nil {
		return nil, fmt.Errorf("objectstore: mkdir %s: %w", objRoot, err)
	}
	return &LocalStore{root: cfg.Root, algo: algo, enc: enc, dec: dec}, nil
}

func zstdLevelFromEnv() int {
	if v := os.Getenv("ORUN_OBJECT_ZSTD_LEVEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 3
}

// Root returns the store root path.
func (l *LocalStore) Root() string { return l.root }

// Algo returns the store's hash algorithm.
func (l *LocalStore) Algo() Algo { return l.algo }

// ModTime returns the on-disk modification time of an object — when its loose
// file was written. It is used by garbage collection's grace window to avoid
// sweeping objects that were just written but whose ref has not moved yet. This
// is a local-driver convenience, not part of the ObjectStore interface (a remote
// driver exposes age differently). Returns ErrNotFound if absent.
func (l *LocalStore) ModTime(_ context.Context, id ObjectID) (time.Time, error) {
	path, err := l.objectPath(id)
	if err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, ErrNotFound
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// objectPath maps an id to its on-disk path: objects/<algo>/<hex[:2]>/<hex[2:]>.
func (l *LocalStore) objectPath(id ObjectID) (string, error) {
	algo, hexpart, err := parseID(id)
	if err != nil {
		return "", err
	}
	if algo != l.algo {
		return "", fmt.Errorf("%w: id algo %q != store algo %q", ErrInvalid, algo, l.algo)
	}
	return filepath.Join(l.root, "objects", string(algo), hexpart[:2], hexpart[2:]), nil
}

// write stores framed bytes under id, compressing and writing atomically. It is
// a no-op when the object already exists (idempotent).
func (l *LocalStore) write(id ObjectID, serialized []byte) error {
	path, err := l.objectPath(id)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return nil // already present
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("objectstore: mkdir %s: %w", dir, err)
	}
	compressed := l.enc.EncodeAll(serialized, nil)
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("objectstore: temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(compressed); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("objectstore: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("objectstore: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("objectstore: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("objectstore: rename: %w", err)
	}
	cleanup = false
	return nil
}

// PutBlob stores data as a blob (idempotent).
func (l *LocalStore) PutBlob(_ context.Context, data []byte) (ObjectID, error) {
	serialized, id, err := computeBlobID(l.algo, data)
	if err != nil {
		return "", err
	}
	if err := l.write(id, serialized); err != nil {
		return "", err
	}
	return id, nil
}

// PutTree validates+sorts entries and stores the tree (idempotent).
func (l *LocalStore) PutTree(_ context.Context, entries []TreeEntry) (ObjectID, error) {
	_, serialized, id, err := computeTree(l.algo, entries)
	if err != nil {
		return "", err
	}
	if err := l.write(id, serialized); err != nil {
		return "", err
	}
	return id, nil
}

// getSerialized reads and decompresses the framed bytes for id.
func (l *LocalStore) getSerialized(id ObjectID) ([]byte, error) {
	path, err := l.objectPath(id)
	if err != nil {
		return nil, err
	}
	compressed, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("objectstore: read %s: %w", path, err)
	}
	serialized, err := l.dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: zstd decode: %v", ErrCorrupt, err)
	}
	return serialized, nil
}

// Get returns the kind and body of id, verifying integrity.
func (l *LocalStore) Get(_ context.Context, id ObjectID) (Kind, []byte, error) {
	serialized, err := l.getSerialized(id)
	if err != nil {
		return "", nil, err
	}
	if err := verify(l.algo, serialized, id); err != nil {
		return "", nil, err
	}
	kind, body, err := parseFrame(serialized)
	if err != nil {
		return "", nil, err
	}
	return kind, body, nil
}

// GetTree decodes a tree object.
func (l *LocalStore) GetTree(ctx context.Context, id ObjectID) ([]TreeEntry, error) {
	kind, body, err := l.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if kind != KindTree {
		return nil, ErrInvalid
	}
	return decodeTreeBody(body, l.algo)
}

// Has reports presence by stat without reading the body.
func (l *LocalStore) Has(_ context.Context, id ObjectID) (bool, error) {
	path, err := l.objectPath(id)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Walk visits every object reachable from root depth-first.
func (l *LocalStore) Walk(ctx context.Context, root ObjectID, fn func(ObjectID, Kind) error) error {
	return walkFrom(ctx, root, l.Get, l.algo, make(map[ObjectID]struct{}), fn)
}

// Iterate enumerates every present object id by walking the on-disk tree.
func (l *LocalStore) Iterate(_ context.Context, fn func(ObjectID) error) error {
	base := filepath.Join(l.root, "objects", string(l.algo))
	hexLen, err := l.algo.hexLen()
	if err != nil {
		return err
	}
	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		// Skip stray temp files from interrupted writes.
		if strings.HasPrefix(name, "tmp-") {
			return nil
		}
		aa := filepath.Base(filepath.Dir(path))
		if len(aa) != 2 {
			return nil
		}
		hexpart := aa + name
		if len(hexpart) != hexLen {
			return nil
		}
		return fn(ObjectID(string(l.algo) + ":" + hexpart))
	})
}

// Delete removes one object (GC only). No-op if absent.
func (l *LocalStore) Delete(_ context.Context, id ObjectID) error {
	path, err := l.objectPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("objectstore: delete %s: %w", path, err)
	}
	return nil
}

var _ ObjectStore = (*LocalStore)(nil)
