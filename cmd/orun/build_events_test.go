package main

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/agent/driver"

	"github.com/sourceplane/orun/internal/scaffold"
)

// The wiring, not the sink (orun-bootstrap-engine BE-O13).
//
// `internal/scaffold` tests what the sink does. What is easy to get wrong here
// is whether it is ever CONSTRUCTED — a reporter nothing builds is a feature
// true of the types and false of every run — and whether a local `orun new`
// quietly grew a dependency on a platform it does not have.

func TestPlatformEventSinkNeedsTheWholeSandboxIdentity(t *testing.T) {
	full := map[string]string{
		"ORUN_CLOUD_API":     "https://api.example",
		"ORUN_ORG_ID":        "org_1",
		"ORUN_SESSION_ID":    "as_1",
		"ORUN_SESSION_TOKEN": "tok",
	}

	t.Run("all four present builds a sink", func(t *testing.T) {
		for k, v := range full {
			t.Setenv(k, v)
		}
		if platformEventSink() == nil {
			t.Fatal("a sandbox with the full identity reports nothing")
		}
	})

	// A LOCAL RUN MUST NOT CARE. Each variable missing on its own is enough:
	// a half-configured environment is not a platform, and guessing the rest
	// would mean an `orun new` on somebody's laptop trying to post a build.
	for missing := range full {
		t.Run("without "+missing, func(t *testing.T) {
			for k, v := range full {
				if k == missing {
					t.Setenv(k, "")
					continue
				}
				t.Setenv(k, v)
			}
			if s := platformEventSink(); s != nil {
				t.Fatalf("built a sink with %s unset", missing)
			}
		})
	}
}

// The renderer is not something the platform gets to change: a build's own
// output is the same whether or not anybody is listening.
func TestFanOutSinkFeedsEveryListenerAndToleratesNil(t *testing.T) {
	var a, b countingSink
	f := fanOutSink{&a, nil, &b}
	f.Emit(context.Background(), scaffold.Event{Seq: 1, State: scaffold.EventDone})
	if a.n != 1 || b.n != 1 {
		t.Fatalf("fan-out dropped an event: a=%d b=%d", a.n, b.n)
	}
}

type countingSink struct{ n int }

func (c *countingSink) Emit(context.Context, scaffold.Event) { c.n++ }

// The driver registry is the thing the control plane names in `--driver`, and
// a driver this process has never heard of is a runner that fails at boot with
// `no driver "bootstrap"`. The unit tests for the driver itself cannot catch
// that — registration happens here, in an init nothing imports for its value.
func TestBootstrapDriverIsRegistered(t *testing.T) {
	d, err := driver.Get("bootstrap")
	if err != nil {
		t.Fatalf("the control plane cannot select the bootstrap driver: %v", err)
	}
	if d.ID() != "bootstrap" {
		t.Fatalf("registered under %q", d.ID())
	}
	// And the ones that were already there still are.
	for _, id := range []string{"stub", driver.ClaudeCodeID} {
		if _, err := driver.Get(id); err != nil {
			t.Fatalf("registering bootstrap displaced %q: %v", id, err)
		}
	}
}
