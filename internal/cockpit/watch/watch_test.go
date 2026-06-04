package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/execmodel"
)

type fakeSource struct {
	calls    int
	statuses []string
	err      error
}

func (f *fakeSource) LoadRun(_ context.Context, _ string) (*execmodel.ExecMetadata, *execmodel.ExecState, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	status := "running"
	if f.calls < len(f.statuses) {
		status = f.statuses[f.calls]
	}
	f.calls++
	return &execmodel.ExecMetadata{ExecID: "x", PlanName: "p", Status: status}, &execmodel.ExecState{}, nil
}

func (f *fakeSource) ListRuns(_ context.Context) ([]execmodel.ExecEntry, error) {
	return nil, nil
}

func TestRunEmitsUntilTerminal(t *testing.T) {
	src := &fakeSource{statuses: []string{"running", "running", "completed"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := Run(ctx, src, Options{ExecID: "x", Interval: 100 * time.Millisecond})

	var got []viewmodel.RunView
	terminal := false
	for u := range ch {
		if u.Err != nil {
			t.Fatalf("unexpected err: %v", u.Err)
		}
		got = append(got, u.View)
		if u.Terminal {
			terminal = true
		}
	}
	if !terminal {
		t.Fatal("expected a terminal update")
	}
	if len(got) < 3 {
		t.Fatalf("expected >=3 updates, got %d", len(got))
	}
	if got[len(got)-1].Status != "completed" {
		t.Fatalf("final status = %q, want completed", got[len(got)-1].Status)
	}
}

func TestRunSurfacesErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("boom")}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ch := Run(ctx, src, Options{ExecID: "x", Interval: 100 * time.Millisecond})
	u := <-ch
	if u.Err == nil {
		t.Fatal("expected error update")
	}
}

func TestRunCancellation(t *testing.T) {
	src := &fakeSource{statuses: []string{"running", "running", "running"}}
	ctx, cancel := context.WithCancel(context.Background())
	ch := Run(ctx, src, Options{ExecID: "x", Interval: 50 * time.Millisecond})
	<-ch // first emission
	cancel()
	// drain
	for range ch {
	}
}
