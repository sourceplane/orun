package githubretry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func fast(t *testing.T) {
	t.Helper()
	prev := Backoff
	Backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { Backoff = prev })
}

func TestTransient(t *testing.T) {
	ctx := context.Background()
	reset := errors.New("read: connection reset by peer")
	cases := []struct {
		status int
		err    error
		want   bool
	}{
		{0, reset, true},
		{429, errors.New("rate"), true},
		{500, errors.New("x"), true},
		{502, errors.New("x"), true},
		{503, errors.New("x"), true},
		{504, errors.New("x"), true},
		{401, errors.New("x"), false},
		{403, errors.New("x"), false},
		{404, errors.New("x"), false},
		{422, errors.New("x"), false},
	}
	for _, c := range cases {
		if got := Transient(ctx, c.status, c.err); got != c.want {
			t.Errorf("Transient(%d, %v) = %v, want %v", c.status, c.err, got, c.want)
		}
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if Transient(done, 0, reset) {
		t.Error("a caller that has given up is not a transient failure")
	}
}

func TestDoAsksAReadAgainAndStopsWhenItAnswers(t *testing.T) {
	fast(t)
	calls := 0
	status, err := Do(context.Background(), http.MethodGet, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("read: connection reset by peer")
		}
		return 200, nil
	})
	if err != nil || status != 200 || calls != 3 {
		t.Fatalf("got %d, %v after %d call(s); want 200 on the third", status, err, calls)
	}
}

func TestDoGivesUpAfterTheBackoff(t *testing.T) {
	fast(t)
	calls := 0
	_, err := Do(context.Background(), http.MethodGet, func() (int, error) { calls++; return 503, errors.New("unavailable") })
	if err == nil || calls != 1+len(Backoff) {
		t.Fatalf("calls = %d, err = %v; want %d attempts then the error", calls, err, 1+len(Backoff))
	}
}

// A write that failed in transit may have happened. Asking again is the
// caller's decision, never this package's.
func TestDoNeverRetriesAWrite(t *testing.T) {
	fast(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		calls := 0
		_, _ = Do(context.Background(), m, func() (int, error) { calls++; return 0, errors.New("reset") })
		if calls != 1 {
			t.Errorf("%s was attempted %d times", m, calls)
		}
	}
}

func TestDoDoesNotRetryAnAnswer(t *testing.T) {
	fast(t)
	calls := 0
	_, _ = Do(context.Background(), http.MethodGet, func() (int, error) { calls++; return 404, errors.New("not found") })
	if calls != 1 {
		t.Fatalf("a 404 is an answer; asked %d times", calls)
	}
}
