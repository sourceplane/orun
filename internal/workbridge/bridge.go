package workbridge

import "github.com/sourceplane/orun/internal/affected"

// AffectedSet is the wire DTO the work-plane auto-linker consumes for one PR.
// JSON is lowerCamelCase to match the cloud (orun.io/v1 conventions); the slices
// are always non-nil so the on-wire shape is stable (`[]`, never `null`).
type AffectedSet struct {
	// PR is the stable pull-request reference, e.g. "sourceplane/orun#412".
	PR string `json:"pr"`
	// Components is the blast radius — identical to `orun catalog affected`'s
	// `affected` field (Result.Affected = DirectlyChanged ∪ Dependents). This is
	// the set the auto-linker overlaps against each task's contract.affects.
	Components []string `json:"components"`
	// Dependents is the transitive reverse-dependency closure, carried for
	// reviewer/owner attribution (design §6.1's reviewer suggestions).
	Dependents []string `json:"dependents"`
}

// FromResult projects an affected.Result into the AffectedSet the cloud
// consumes. It copies the engine's already-sorted Affected/Dependents verbatim
// (nil → empty), so the bridge's blast radius equals the engine's by
// construction — the same value `orun catalog affected --json` emits.
func FromResult(pr string, r affected.Result) AffectedSet {
	return AffectedSet{
		PR:         pr,
		Components: nonNil(r.Affected),
		Dependents: nonNil(r.Dependents),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
