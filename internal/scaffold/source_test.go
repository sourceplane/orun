package scaffold

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func TestResolveDirPinsAndReproduces(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha")
	writeFile(t, dir, "sub/b.txt", "beta")

	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	rs1, err := resolveSource(ctx, store, SourceSpec{Name: "s", Kind: SourceDir, Path: dir}, nil, "", t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Same tree ⇒ same digest (reproducible, design §5).
	store2 := objectstore.NewMemStore(objectstore.AlgoSHA256)
	rs2, err := resolveSource(ctx, store2, SourceSpec{Name: "s", Kind: SourceDir, Path: dir}, nil, "", t.TempDir())
	if err != nil {
		t.Fatalf("resolve2: %v", err)
	}
	if rs1.Digest != rs2.Digest {
		t.Fatalf("digest not reproducible: %s != %s", rs1.Digest, rs2.Digest)
	}

	// The pinned tree reads back the same content via the store-backed view.
	got, err := rs1.Tree.ReadFile("sub/b.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "beta" {
		t.Fatalf("read = %q", got)
	}
	files, err := rs1.Tree.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("list = %v", files)
	}
}

func TestSourceAgnosticPlacement(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "svc/config.yaml", "name: {{ .name }}")

	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	rs, err := resolveSource(ctx, store, SourceSpec{Name: "base", Kind: SourceDir, Path: dir}, nil, "", t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	m := Module{Name: "svc", Mode: ModeTemplate, Source: "base", From: "svc", To: "out", Bind: []string{"config.yaml"}}
	vals := mkValues(map[string]any{"name": "gamma"})

	// Placement from the store-backed tree.
	fromStore, err := placeModule(m, rs.Tree, vals)
	if err != nil {
		t.Fatalf("place store: %v", err)
	}
	// Placement from a raw osTree over the same dir must be byte-identical.
	fromDir, err := placeModule(m, osTree{root: dir}, vals)
	if err != nil {
		t.Fatalf("place dir: %v", err)
	}
	if !placedEqual(fromStore.files, fromDir.files) {
		t.Fatal("placement differs between store-backed and dir-backed source (not source-agnostic)")
	}
	if len(fromStore.files) != 1 || fromStore.files[0].Path != "out/config.yaml" || string(fromStore.files[0].Bytes) != "name: gamma" {
		t.Fatalf("unexpected placement: %+v", fromStore.files)
	}
}

func TestIgnoreExcludesFromDigestAndPlacement(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "src/app.ts", "code")
	writeFile(t, dir, ".next/build/chunk.js", "artifact")
	writeFile(t, dir, "dist/out.js", "artifact")

	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	// With ignore, build artifacts are excluded from the source view.
	rs, err := resolveSource(ctx, store, SourceSpec{Name: "b", Kind: SourceDir, Path: dir}, []string{".next", "dist"}, "", t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	files, err := rs.Tree.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0] != "src/app.ts" {
		t.Fatalf("ignore not honored, got %v", files)
	}
	// Digest is stable regardless of the artifacts' content (they're excluded).
	writeFile(t, dir, ".next/build/chunk.js", "DIFFERENT artifact")
	store2 := objectstore.NewMemStore(objectstore.AlgoSHA256)
	rs2, _ := resolveSource(ctx, store2, SourceSpec{Name: "b", Kind: SourceDir, Path: dir}, []string{".next", "dist"}, "", t.TempDir())
	if rs.Digest != rs2.Digest {
		t.Fatalf("ignored content leaked into digest: %s != %s", rs.Digest, rs2.Digest)
	}
}
