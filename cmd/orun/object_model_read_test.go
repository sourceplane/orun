package main

import (
	"testing"
)

func TestOpenObjectReaderOffByDefault(t *testing.T) {
	t.Setenv("ORUN_OBJECT_RUNNER", "")
	t.Setenv("ORUN_OBJECT_MODEL", "")
	if _, ok := openObjectReader(); ok {
		t.Fatalf("openObjectReader should be off when flags unset")
	}
}

func TestOpenObjectReaderAbsentStore(t *testing.T) {
	t.Setenv("ORUN_OBJECT_RUNNER", "1")
	t.Chdir(t.TempDir()) // no .orun/objectmodel/objects here
	if _, ok := openObjectReader(); ok {
		t.Fatalf("openObjectReader should be off when the object model is absent")
	}
}
