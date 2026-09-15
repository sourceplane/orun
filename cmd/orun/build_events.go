package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/scaffold"
)

// Reporting a build to the platform that is showing it to somebody
// (orun-bootstrap-engine BE-O13).
//
// `orun new` already renders its events to stdout. Inside a platform sandbox
// there is a second consumer: orun-cloud's build page, which has a store, an
// ingest door, a narration feed and a `blocked` row state — all of them dark
// until something sends. This is what turns the environment the control plane
// already injects into that delivery.
//
// # The run id is the SESSION's, not the engine's
//
// `scaffold.Event.RunID` is minted per `orun new` invocation and is internal to
// the build. The platform keys its stream on the id the bootstrap door named,
// which is what the console reads back. Sending under the engine's id would
// store a perfectly good stream nobody can find — an empty build page and
// nothing anywhere saying why. So the session id is the path, and the engine's
// run id is not sent at all.
//
// # Absent is the normal case
//
// A local `orun new` has no platform, and must not be made to care. Every
// variable missing means a nil sink, which `events.go` already defines as
// "nobody is listening".

// platformEventSink builds the sink for this process, or nil when there is no
// platform to report to. `logf` receives failures — never fatal: the build is
// the point and the feed is the report.
func platformEventSink() *scaffold.PlatformSink {
	api := os.Getenv("ORUN_CLOUD_API")
	org := os.Getenv("ORUN_ORG_ID")
	session := os.Getenv("ORUN_SESSION_ID")
	token := os.Getenv("ORUN_SESSION_TOKEN")
	if api == "" || org == "" || session == "" || token == "" {
		return nil
	}
	client := remotestate.NewClient(api, version, remotestate.NewStaticTokenSource(token))
	return scaffold.NewPlatformSink(
		buildEventPoster{client: client},
		org,
		session,
		func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		},
	)
}

// buildEventPoster adapts the platform client to the engine's narrow need.
// `internal/scaffold` must not import `internal/remotestate` — the engine does
// not know it is hosted — so the adapter lives on this side of the seam.
type buildEventPoster struct{ client *remotestate.Client }

func (p buildEventPoster) AppendBuildEvents(
	ctx context.Context, org, runID string, events []scaffold.PlatformBuildEvent,
) (int, error) {
	wire := make([]remotestate.BuildEvent, len(events))
	for i, e := range events {
		wire[i] = remotestate.BuildEvent{
			Seq:       e.Seq,
			At:        e.At,
			Phase:     e.Phase,
			Step:      e.Step,
			State:     e.State,
			Narration: e.Narration,
			Detail:    e.Detail,
			Meta:      e.Meta,
		}
	}
	res, err := p.client.AppendBuildEvents(ctx, org, runID, wire)
	if err != nil {
		return 0, err
	}
	return res.LatestSeq, nil
}

// fanOutSink writes every event to each sink in turn. The stdout renderer stays
// exactly what it was — a build's own output is not something the platform gets
// to change — and the platform sink is added beside it.
type fanOutSink []scaffold.EventSink

func (f fanOutSink) Emit(ctx context.Context, e scaffold.Event) {
	for _, s := range f {
		if s != nil {
			s.Emit(ctx, e)
		}
	}
}
