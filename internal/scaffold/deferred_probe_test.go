package scaffold

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/actions"
	"github.com/sourceplane/orun/internal/objectstore"
)

// Phase 02 asks for something phase 01 publishes — the shape of every real
// bootstrap: 04-workers wants the D1 and KV bindings 03-infrastructure
// creates.
const twoPhaseProbeBlueprint = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
modules:
  - name: first
    mode: template
    files:
      one.md: "1"
  - name: second
    mode: template
    files:
      two.md: "2"
phases:
  - name: 01-publish
    modules: [first]
    hooks:
      post:
        - id: publish
          uses: orun.http/probe@v1
          with:
            urls: ["https://example.invalid/health"]
  - name: 02-consume
    modules: [second]
    requires:
      phases: [01-publish]
      probe:
        - id: wiring
          uses: orun.secrets/exists@v1
          with:
            keys: ["WIRING_CLOUDFLARE_D1"]
`

// publishThenReady answers the probe the way reality does: not yet, until the
// phase that publishes has actually run.
type publishThenReady struct {
	mu        sync.Mutex
	published bool
	probes    int
}

func (p *publishThenReady) Run(_ context.Context, id string, _ ActionInput) (map[string]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id == "orun.secrets/exists@v1" {
		p.probes++
		if !p.published {
			return nil, &actions.PendingError{ID: "wiring", Pending: actions.Pending{Reason: "secret(s) not published yet: WIRING_CLOUDFLARE_D1"}}
		}
		return nil, nil
	}
	p.published = true // the publishing hook ran
	return nil, nil
}

// THE BOOTSTRAP THAT COULD NOT RUN ITSELF. Preconditions are swept up front,
// before a byte is written — correct for a probe about the outside world, and
// impossible for one about what an earlier phase in this same run is about to
// do. Up there the answer is always "not yet", and the sweep called that a
// failed precondition:
//
//	✕ phase "04-workers" precondition "wiring" is not met: … pending —
//	  secret(s) not published yet: WIRING_CLOUDFLARE_D1, WIRING_CLOUDFLARE_KV
//
// Deferred instead, the probe is asked again once 01 has run, and answers yes.
func TestAPendingProbeIsDeferredUntilThePhaseBeforeItHasRun(t *testing.T) {
	r := &publishThenReady{}
	if _, err := Run(context.Background(), Options{
		Blueprint: []byte(twoPhaseProbeBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: r,
		RunHooks: true,
	}); err != nil {
		t.Fatalf("a probe its own run satisfies must not fail the run: %v", err)
	}
	if r.probes < 2 {
		t.Fatalf("probed %d time(s): the deferred probe must be asked again at phase time", r.probes)
	}
}

// neverReady never publishes: the probe stays pending forever.
type neverReady struct{ probes int }

func (n *neverReady) Run(_ context.Context, id string, _ ActionInput) (map[string]string, error) {
	if id == "orun.secrets/exists@v1" {
		n.probes++
		return nil, &actions.PendingError{ID: "wiring", Pending: actions.Pending{Reason: "secret(s) not published yet: WIRING_CLOUDFLARE_D1"}}
	}
	return nil, nil
}

// Deferring is not forgiving. A probe still pending when its phase arrives
// PARKS the run — the same answer a pending hook gets — so `--resume` picks it
// up when the thing it waits for exists, rather than the phase proceeding
// without what it declared it needs.
func TestAProbeStillPendingAtPhaseTimeParksTheRun(t *testing.T) {
	r := &neverReady{}
	_, err := Run(context.Background(), Options{
		Blueprint: []byte(twoPhaseProbeBlueprint), OutDir: t.TempDir(),
		Store: objectstore.NewMemStore(objectstore.AlgoSHA256), Actions: r,
		RunHooks: true,
	})
	if err == nil {
		t.Fatal("a probe that never comes good must not let its phase run")
	}
	parked := &ParkedError{}
	if !errors.As(err, &parked) {
		t.Fatalf("error = %#v, want a ParkedError the run can resume from", err)
	}
	if !strings.Contains(parked.Reason, "not published yet") {
		t.Fatalf("parked reason = %q, want the probe's own words", parked.Reason)
	}
	if parked.Phase != "02-consume" {
		t.Fatalf("parked phase = %q, want the phase that asked", parked.Phase)
	}
}

// A probe that fails OUTRIGHT — not pending — still fails the run up front,
// before anything is written. "Not yet" and "never" must not trade places.
func TestAHardProbeFailureStillStopsTheRunBeforeWriting(t *testing.T) {
	out := t.TempDir()
	_, err := Run(context.Background(), Options{
		Blueprint: []byte(twoPhaseProbeBlueprint), OutDir: out,
		Store:    objectstore.NewMemStore(objectstore.AlgoSHA256),
		Actions:  &flakyRunner{failures: 99},
		RunHooks: true,
	})
	if err == nil {
		t.Fatal("a probe that fails outright must stop the run")
	}
	if !strings.Contains(err.Error(), "precondition") {
		t.Fatalf("error = %v, want the gate's own wording", err)
	}
	if _, statErr := os.Stat(out + "/one.md"); statErr == nil {
		t.Fatal("the run wrote files despite a failed precondition")
	}
}
