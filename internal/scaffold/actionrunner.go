package scaffold

import (
	"context"

	"github.com/sourceplane/orun/internal/actions"
)

// The action seam (orun-bootstrap-engine BE-O1).
//
// The engine calls an ActionRunner; it does not reach into an implementation.
// Two reasons, and the second is the one that pays off repeatedly:
//
//  1. Actions do the things the render core is structurally forbidden to do —
//     network, clock, process. Keeping them behind an interface keeps that
//     boundary visible rather than incidental.
//  2. A test — and a baseline's phase-simulation CI tier — can substitute a
//     recording runner and assert the exact sequence of actions a phase
//     performs, with their parameters, without a network or a credential.
//
// Parse-time VALIDATION still consults the registry directly: it is a pure
// lookup over declared specs, it has to happen inside ParseBlueprint where
// there is nothing to inject through, and a blueprint must validate identically
// no matter who later runs it.

// ActionInput is what an action receives. It mirrors actions.Input so callers
// of this package need not import the registry to drive the engine.
type ActionInput struct {
	Dir     string
	BaseDir string
	Params  map[string]any
}

// ActionRunner executes a registered action by id and returns its outputs.
type ActionRunner interface {
	Run(ctx context.Context, id string, in ActionInput) (map[string]string, error)
}

// registryRunner is the default: the real, closed registry.
type registryRunner struct{}

func (registryRunner) Run(ctx context.Context, id string, in ActionInput) (map[string]string, error) {
	res, err := actions.Run(ctx, id, actions.Input{Dir: in.Dir, BaseDir: in.BaseDir, Params: in.Params})
	if err != nil {
		return res.Outputs, err
	}
	return res.Outputs, nil
}

// DefaultActionRunner returns the registry-backed runner used when a caller
// configures none.
func DefaultActionRunner() ActionRunner { return registryRunner{} }
