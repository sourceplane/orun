package model

import (
	"strings"
	"testing"
)

func TestCacheDeclValidate(t *testing.T) {
	t.Parallel()
	for _, ok := range []CacheDecl{
		{Name: "next-web", Path: ".next/cache"},
		{Name: "playwright", Path: "node_modules/.cache/ms-playwright"},
		{Name: "a.b_c", Path: "dist"},
	} {
		if err := ok.Validate("build"); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []struct {
		decl CacheDecl
		want string
	}{
		{CacheDecl{Name: "", Path: "x"}, "cache name"},
		{CacheDecl{Name: "../x", Path: "x"}, "cache name"},
		{CacheDecl{Name: "ok", Path: ""}, "no path"},
		{CacheDecl{Name: "ok", Path: "/tmp/x"}, "relative"},
		{CacheDecl{Name: "ok", Path: "../sibling"}, "inside"},
		{CacheDecl{Name: "ok", Path: "."}, "inside"},
	} {
		err := bad.decl.Validate("build")
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%+v: err = %v, want %q", bad.decl, err, bad.want)
		}
	}
	step := Step{Name: "build", Run: "x", Caches: []CacheDecl{{Name: "ok", Path: "dist"}, {Name: "bad", Path: "/abs"}}}
	if err := step.ValidateCaches(); err == nil {
		t.Fatal("a step with an invalid cache fails validation")
	}
}
