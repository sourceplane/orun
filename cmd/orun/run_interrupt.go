package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sourceplane/orun/internal/ui"
)

// interruptContext is the run's root context, cancelled by SIGTERM (a spot
// runner's interruption notice forwarded by the pool, a cancelled CI job) or
// SIGINT (Ctrl-C). interrupted reports the signal once one arrived; stop
// releases the signal handler. A second signal while draining exits at
// once, the usual escape hatch (specs/orun-runner-efficiency §5.6).
func interruptContext(stderr io.Writer) (ctx context.Context, interrupted func() os.Signal, stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	var mu sync.Mutex
	var got os.Signal
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				mu.Lock()
				first := got == nil
				if first {
					got = s
				}
				mu.Unlock()
				if !first {
					fmt.Fprintf(stderr, "\n%s %s again: exiting now\n", ui.Yellow(ui.ColorEnabledForWriter(stderr), "↯"), s)
					os.Exit(exitCodeFor(s))
				}
				fmt.Fprintf(stderr, "\n%s %s: draining — stopping running steps, admitting no further job (again to exit now)\n", ui.Yellow(ui.ColorEnabledForWriter(stderr), "↯"), s)
				cancel()
			case <-done:
				return
			}
		}
	}()
	return ctx,
		func() os.Signal {
			mu.Lock()
			defer mu.Unlock()
			return got
		},
		func() {
			signal.Stop(sigs)
			close(done)
			cancel()
		}
}

// interruptedError is the run's error after a signal; its exit code is the
// shell's for that signal (130 for SIGINT, 143 for SIGTERM).
type interruptedError struct {
	err error
	sig os.Signal
}

func (e *interruptedError) Error() string { return e.err.Error() }
func (e *interruptedError) Unwrap() error { return e.err }
func (e *interruptedError) ExitCode() int { return exitCodeFor(e.sig) }

func exitCodeFor(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 143
	}
	return 130
}
