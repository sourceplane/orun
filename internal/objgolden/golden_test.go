package objgolden

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

var update = flag.Bool("update", false, "regenerate the golden-vectors.json fixture")

const goldenPath = "testdata/golden-vectors.json"

// ── The language-neutral fixture schema (mirrored by the TS consumer) ──

type relation struct {
	Type      string `json:"type"`
	TargetRef string `json:"targetRef"`
}

// projected is the minimal org-catalog projection both readers must agree on —
// the shape orun-cloud's catalog-projection.ts ProjectedEntity carries. owner /
// lifecycle are nullable (a pointer here → JSON null → TS `string | null`).
type projected struct {
	EntityRef string     `json:"entityRef"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	Owner     *string    `json:"owner"`
	Lifecycle *string    `json:"lifecycle"`
	Relations []relation `json:"relations"`
}

type vector struct {
	Name string `json:"name"`
	// RootDigest is the catalog snapshot root ("sha256:<hex>").
	RootDigest string `json:"rootDigest"`
	// Frames maps every object id to its hex-encoded framed bytes
	// ("<kind> <len>\x00<body>") — the exact bytes the platform stores in R2.
	Frames map[string]string `json:"frames"`
	// Expected is the projection a faithful reader must produce, sorted.
	Expected []projected `json:"expected"`
}

type suite struct {
	Source  string   `json:"_source"`
	Vectors []vector `json:"vectors"`
}

// ── Projection rules — the single shared definition (used to compute the
// expected projection AND to re-project the decoded frames). Mirrors
// catalog-projection.ts componentEntity / derivedEntity exactly. ──

func firstString(m map[string]any, keys ...string) *string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return &s
		}
	}
	return nil
}

func projectComponent(m nodes.ComponentManifest) (projected, bool) {
	ref := m.Identity.ComponentKey
	if ref == "" {
		return projected{}, false
	}
	name := m.Identity.Name
	if name == "" {
		name = ref
	}
	rels := []relation{}
	for _, r := range m.Relations {
		if r.Type != "" && r.To != "" {
			rels = append(rels, relation{Type: r.Type, TargetRef: r.To})
		}
	}
	return projected{
		EntityRef: ref,
		Kind:      "Component",
		Name:      name,
		Owner:     firstString(m.Ownership, "owner"),
		Lifecycle: firstString(m.Lifecycle, "stage", "lifecycle"),
		Relations: rels,
	}, true
}

func projectEntity(e nodes.Entity) (projected, bool) {
	ref := e.Identity.EntityKey
	if ref == "" {
		return projected{}, false
	}
	kind := e.Kind
	if kind == "" {
		kind = e.Identity.Kind
	}
	if kind == "" {
		return projected{}, false
	}
	name := e.Identity.Name
	if name == "" {
		name = ref
	}
	return projected{
		EntityRef: ref,
		Kind:      kind,
		Name:      name,
		Owner:     firstString(e.Ownership, "owner"),
		Lifecycle: firstString(e.Lifecycle, "stage", "lifecycle"),
		Relations: []relation{}, // derived entities carry no relations (parity with TS)
	}, true
}

func sortProjected(ps []projected) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].EntityRef != ps[j].EntityRef {
			return ps[i].EntityRef < ps[j].EntityRef
		}
		return ps[i].Kind < ps[j].Kind
	})
}

// ── Authoring: harvest real frames from objectstore via a capturing transport ──

type captureTransport struct{ frames map[string][]byte }

func (c *captureTransport) HasObject(context.Context, string) (bool, error) { return false, nil }
func (c *captureTransport) GetObject(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}
func (c *captureTransport) PutObject(_ context.Context, digest, _ string, framed []byte) error {
	c.frames[digest] = append([]byte(nil), framed...)
	return nil
}

type replayTransport struct{ frames map[string][]byte }

func (r replayTransport) HasObject(_ context.Context, d string) (bool, error) {
	_, ok := r.frames[d]
	return ok, nil
}
func (r replayTransport) GetObject(_ context.Context, d string) ([]byte, bool, error) {
	b, ok := r.frames[d]
	return b, ok, nil
}
func (replayTransport) PutObject(context.Context, string, string, []byte) error { return nil }

// builder assembles a catalog tree and records the harvested frames.
type builder struct {
	store  *objectstore.RemoteStore
	frames map[string][]byte
}

func newBuilder() *builder {
	cap := &captureTransport{frames: map[string][]byte{}}
	return &builder{store: objectstore.NewRemoteStore(cap, objectstore.AlgoSHA256, ""), frames: cap.frames}
}

func (b *builder) blob(t *testing.T, v any) objectstore.ObjectID {
	t.Helper()
	body, err := nodes.Encode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	id, err := b.store.PutBlob(context.Background(), body)
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	return id
}

func (b *builder) tree(t *testing.T, entries []objectstore.TreeEntry) objectstore.ObjectID {
	t.Helper()
	id, err := b.store.PutTree(context.Background(), entries)
	if err != nil {
		t.Fatalf("put tree: %v", err)
	}
	return id
}

func ent(name string, kind objectstore.Kind, id objectstore.ObjectID) objectstore.TreeEntry {
	return objectstore.TreeEntry{Name: name, Kind: kind, ID: id}
}

func framesToHex(in map[string][]byte) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = hex.EncodeToString(v)
	}
	return out
}

// ── Re-projection from frames via the REAL objectstore deframe + nodes decode ──

func projectFromFrames(t *testing.T, framesHex map[string]string, root string) []projected {
	t.Helper()
	frames := make(map[string][]byte, len(framesHex))
	for k, v := range framesHex {
		b, err := hex.DecodeString(v)
		if err != nil {
			t.Fatalf("hex: %v", err)
		}
		frames[k] = b
	}
	store := objectstore.NewRemoteStore(replayTransport{frames: frames}, objectstore.AlgoSHA256, "")
	ctx := context.Background()
	out := []projected{}

	rootEntries, err := store.GetTree(ctx, objectstore.ObjectID(root))
	if err != nil {
		t.Fatalf("get root tree: %v", err)
	}
	for _, e := range rootEntries {
		switch {
		case e.Name == "components" && e.Kind == objectstore.KindTree:
			blobs, err := store.GetTree(ctx, e.ID)
			if err != nil {
				t.Fatalf("components tree: %v", err)
			}
			for _, b := range blobs {
				if b.Kind != objectstore.KindBlob {
					continue
				}
				_, body, err := store.Get(ctx, b.ID)
				if err != nil {
					t.Fatalf("get component: %v", err)
				}
				m, err := nodes.Decode[nodes.ComponentManifest](body)
				if err != nil {
					t.Fatalf("decode component: %v", err)
				}
				if p, ok := projectComponent(m); ok {
					out = append(out, p)
				}
			}
		case e.Name == "entities" && e.Kind == objectstore.KindTree:
			kinds, err := store.GetTree(ctx, e.ID)
			if err != nil {
				t.Fatalf("entities tree: %v", err)
			}
			for _, kt := range kinds {
				if kt.Kind != objectstore.KindTree {
					continue
				}
				blobs, err := store.GetTree(ctx, kt.ID)
				if err != nil {
					t.Fatalf("entity kind tree: %v", err)
				}
				for _, b := range blobs {
					if b.Kind != objectstore.KindBlob {
						continue
					}
					_, body, err := store.Get(ctx, b.ID)
					if err != nil {
						t.Fatalf("get entity: %v", err)
					}
					ev, err := nodes.Decode[nodes.Entity](body)
					if err != nil {
						t.Fatalf("decode entity: %v", err)
					}
					if p, ok := projectEntity(ev); ok {
						out = append(out, p)
					}
				}
			}
		}
	}
	sortProjected(out)
	return out
}

// ── The fixtures ──

func ptr(s string) *string { return &s }

func buildSuite(t *testing.T) suite {
	t.Helper()

	// Vector 1 — a populated catalog: two components (one full, one minimal with
	// null owner/lifecycle) + two derived entities under entities/<Kind>/, plus
	// non-catalog root entries (catalog.json, relations.json, a graph/ tree) that
	// a faithful reader must IGNORE.
	full := newBuilder()
	api := full.blob(t, nodes.ComponentManifest{
		Kind:      "Component",
		Identity:  nodes.ComponentIdentity{ComponentKey: "default/repo/api", Name: "api"},
		Ownership: map[string]any{"owner": "team-platform", "source": "authored"},
		Lifecycle: map[string]any{"stage": "production"},
		Relations: []nodes.EntityRelation{{Type: "dependsOn", To: "default/repo/db", ToKind: "Component"}},
	})
	web := full.blob(t, nodes.ComponentManifest{
		Kind:     "Component",
		Identity: nodes.ComponentIdentity{ComponentKey: "default/repo/web", Name: "web"},
	})
	components := full.tree(t, []objectstore.TreeEntry{
		ent("api.json", objectstore.KindBlob, api),
		ent("web.json", objectstore.KindBlob, web),
	})
	apiEntity := full.blob(t, nodes.Entity{
		Kind:      "API",
		Identity:  nodes.EntityIdentity{EntityKey: "default/repo/api-grpc", Kind: "API", Name: "api-grpc"},
		Ownership: map[string]any{"owner": "team-platform"},
		Lifecycle: map[string]any{"stage": "production"},
	})
	resEntity := full.blob(t, nodes.Entity{
		Kind:     "Resource",
		Identity: nodes.EntityIdentity{EntityKey: "default/repo/pg", Kind: "Resource", Name: "pg"},
	})
	apiKind := full.tree(t, []objectstore.TreeEntry{ent("api-grpc.json", objectstore.KindBlob, apiEntity)})
	resKind := full.tree(t, []objectstore.TreeEntry{ent("pg.json", objectstore.KindBlob, resEntity)})
	entities := full.tree(t, []objectstore.TreeEntry{
		ent("API", objectstore.KindTree, apiKind),
		ent("Resource", objectstore.KindTree, resKind),
	})
	catBlob := full.blob(t, map[string]any{"kind": "catalog-snapshot", "componentCount": 2})
	relBlob := full.blob(t, map[string]any{"kind": "relation-graph", "edges": []any{}})
	graphTree := full.tree(t, []objectstore.TreeEntry{ent("dependencies.json", objectstore.KindBlob, relBlob)})
	fullRoot := full.tree(t, []objectstore.TreeEntry{
		ent("catalog.json", objectstore.KindBlob, catBlob),
		ent("components", objectstore.KindTree, components),
		ent("entities", objectstore.KindTree, entities),
		ent("graph", objectstore.KindTree, graphTree),
		ent("relations.json", objectstore.KindBlob, relBlob),
	})

	// Vector 2 — only non-catalog entries: a faithful reader projects nothing.
	empty := newBuilder()
	eCat := empty.blob(t, map[string]any{"kind": "catalog-snapshot", "componentCount": 0})
	emptyRoot := empty.tree(t, []objectstore.TreeEntry{
		ent("catalog.json", objectstore.KindBlob, eCat),
	})

	return suite{
		Source: "github.com/sourceplane/orun/internal/objgolden",
		Vectors: []vector{
			{
				Name:       "populated-catalog-ignores-non-entity-entries",
				RootDigest: string(fullRoot),
				Frames:     framesToHex(full.frames),
				Expected: func() []projected {
					ps := []projected{
						{EntityRef: "default/repo/api", Kind: "Component", Name: "api", Owner: ptr("team-platform"), Lifecycle: ptr("production"), Relations: []relation{{Type: "dependsOn", TargetRef: "default/repo/db"}}},
						{EntityRef: "default/repo/web", Kind: "Component", Name: "web", Owner: nil, Lifecycle: nil, Relations: []relation{}},
						{EntityRef: "default/repo/api-grpc", Kind: "API", Name: "api-grpc", Owner: ptr("team-platform"), Lifecycle: ptr("production"), Relations: []relation{}},
						{EntityRef: "default/repo/pg", Kind: "Resource", Name: "pg", Owner: nil, Lifecycle: nil, Relations: []relation{}},
					}
					sortProjected(ps)
					return ps
				}(),
			},
			{
				Name:       "only-non-catalog-entries-projects-nothing",
				RootDigest: string(emptyRoot),
				Frames:     framesToHex(empty.frames),
				Expected:   []projected{},
			},
		},
	}
}

func TestGoldenVectors(t *testing.T) {
	s := buildSuite(t)

	if *update {
		blob, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, append(blob, '\n'), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
	}

	// 1) The committed fixture must match what the builder produces now (drift /
	//    forgot-to-regenerate guard).
	committed, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	want, _ := json.MarshalIndent(s, "", "  ")
	if string(committed) != string(want)+"\n" {
		t.Fatalf("%s is stale — regenerate with: go test ./internal/objgolden/ -update", goldenPath)
	}

	// 2) The Go reader (objectstore deframe + nodes decode + projection) must
	//    reproduce the expected projection from the frames alone.
	for _, v := range s.Vectors {
		got := projectFromFrames(t, v.Frames, v.RootDigest)
		want := append([]projected{}, v.Expected...)
		sortProjected(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("vector %q: projection mismatch\n got: %+v\nwant: %+v", v.Name, got, want)
		}
	}
}
