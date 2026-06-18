package remotestate

import (
	"context"
	"strings"
	"testing"
)

func TestAdvanceCatalogHead(t *testing.T) {
	ctx := context.Background()
	client, cloud := newCloudClient(t)

	digest := "sha256:" + strings.Repeat("a", 64)
	cloud.objects[digest] = []byte("tree 0\x00") // the snapshot must exist in the object plane

	head, prev, err := client.AdvanceCatalogHead(ctx, digest, "prod", "abc123")
	if err != nil {
		t.Fatalf("first advance: %v", err)
	}
	if head.Digest != digest || head.Environment != "prod" || head.Commit != "abc123" {
		t.Fatalf("unexpected head: %+v", head)
	}
	if prev != nil {
		t.Fatalf("first advance should have no previous, got %+v", prev)
	}

	// A second advance returns the head it replaced.
	digest2 := "sha256:" + strings.Repeat("b", 64)
	cloud.objects[digest2] = []byte("tree 0\x00")
	head2, prev2, err := client.AdvanceCatalogHead(ctx, digest2, "prod", "def456")
	if err != nil {
		t.Fatalf("second advance: %v", err)
	}
	if head2.Digest != digest2 {
		t.Fatalf("unexpected head2 digest: %s", head2.Digest)
	}
	if prev2 == nil || prev2.Digest != digest {
		t.Fatalf("expected previous head %s, got %+v", digest, prev2)
	}
}

func TestAdvanceCatalogHeadObjectMissing(t *testing.T) {
	ctx := context.Background()
	client, _ := newCloudClient(t)

	// The snapshot was never uploaded → 412 object_missing → descriptive error.
	_, _, err := client.AdvanceCatalogHead(ctx, "sha256:"+strings.Repeat("c", 64), "", "")
	if err == nil {
		t.Fatal("expected an error when the snapshot is not uploaded")
	}
	if !strings.Contains(err.Error(), "not uploaded") {
		t.Fatalf("expected an actionable 'not uploaded' error, got: %v", err)
	}
}
