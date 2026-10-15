package main

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// A head this client advanced to a snapshot is not advanced to it again;
// the memory is per scope (specs/orun-sync-efficiency F12).
func TestHeadAlreadyPushed_RemembersPerScope(t *testing.T) {
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: t.TempDir(), Writer: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d1 := "sha256:" + "11" + "00000000000000000000000000000000000000000000000000000000000000"
	d2 := "sha256:" + "22" + "00000000000000000000000000000000000000000000000000000000000000"
	if headAlreadyPushed(ctx, refs, "", d1) {
		t.Fatal("nothing pushed yet")
	}
	rememberPushedHead(ctx, refs, "", d1)
	if !headAlreadyPushed(ctx, refs, "", d1) {
		t.Fatal("the project-wide head was pushed to d1")
	}
	if headAlreadyPushed(ctx, refs, "", d2) || headAlreadyPushed(ctx, refs, "production", d1) {
		t.Fatal("another snapshot, or another scope, is not remembered")
	}
	rememberPushedHead(ctx, refs, "", d2)
	if headAlreadyPushed(ctx, refs, "", d1) || !headAlreadyPushed(ctx, refs, "", d2) {
		t.Fatal("the memory moves with the head")
	}
	if headAlreadyPushed(ctx, nil, "", d2) {
		t.Fatal("no refs, no memory")
	}
	if catalogPushedRef("") != "catalogs/pushed/project" || catalogPushedRef(" staging ") != "catalogs/pushed/staging" {
		t.Fatal(catalogPushedRef(""), catalogPushedRef(" staging "))
	}
}
