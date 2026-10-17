package outtee

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type failing struct{ calls int }

func (f *failing) Write(p []byte) (int, error) { f.calls++; return 0, errors.New("down") }

func TestCaptureTeesToTheSinkAndKeepsTheRecord(t *testing.T) {
	var rec, live bytes.Buffer
	w := Capture(With(context.Background(), &live), &rec)
	_, _ = w.Write([]byte("one\n"))
	_, _ = w.Write([]byte("two\n"))
	if rec.String() != "one\ntwo\n" || live.String() != "one\ntwo\n" {
		t.Fatalf("rec %q live %q", rec.String(), live.String())
	}
}

func TestCaptureWithoutASinkIsTheBuffer(t *testing.T) {
	var rec bytes.Buffer
	if Capture(context.Background(), &rec) != &rec {
		t.Fatal("no sink should return the buffer itself")
	}
	if From(nil) != nil || With(context.Background(), nil) == nil {
		t.Fatal("nil handling")
	}
}

func TestABrokenSinkNeverFailsTheStep(t *testing.T) {
	var rec bytes.Buffer
	sink := &failing{}
	w := Capture(With(context.Background(), sink), &rec)
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	if rec.String() != "xxx" || sink.calls != 1 {
		t.Fatalf("rec %q sink calls %d", rec.String(), sink.calls)
	}
}
