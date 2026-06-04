package main

import (
	"testing"
)

func TestOpenObjectReaderAbsentStore(t *testing.T) {
	t.Chdir(t.TempDir()) // no .orun/objectmodel/objects here
	if _, ok := openObjectReader(); ok {
		t.Fatalf("openObjectReader should be off when the object model is absent")
	}
}
