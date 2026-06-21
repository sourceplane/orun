package statebackend

// Client runner driver (NC2) — the CLI counterpart to the server's coordinator
// deciders. Given the outcome of a claim or heartbeat (per coordination-api.md
// §3), decide the runner's next action. Pure: the loop wiring (HTTP append +
// folding the log) lives in the remote client, but the decisions are unit-tested
// here so the CLI reacts to the contract identically to how the server enforces
// it.

// ClaimOutcomeKind is the shape of a :claim response.
type ClaimOutcomeKind int

const (
	OutcomeClaimed  ClaimOutcomeKind = iota // we won the job — execute it
	OutcomeCached                           // memoized hit — adopt the result, skip execution
	OutcomeRejected                         // not claimed — see Reason
)

// ClaimOutcome is the decoded result of a :claim append attempt.
type ClaimOutcome struct {
	Kind           ClaimOutcomeKind
	Reason         string // when Rejected: deps_not_ready | job_held | run_terminal
	LeaseEpoch     int
	LeaseExpiresAt string
	Attempt        int // claim attempt count; >1 means a takeover of a lapsed lease
	// Server-supplied lease tunables (contract §3 claim response), so the runner
	// never hardcodes the heartbeat cadence.
	LeaseSeconds             int
	HeartbeatIntervalSeconds int
	ResultDigest             string // when Cached
}

// RunnerAction is what the runner loop should do next for a job.
type RunnerAction int

const (
	ActionExecute     RunnerAction = iota // run the job (we hold the lease)
	ActionAdoptCached                     // adopt the memoized result, do not execute
	ActionWaitDeps                        // deps not ready — wait on the log, then retry
	ActionSkip                            // another runner holds it or it is already terminal
	ActionStop                            // our lease is lost — stop work on this job
)

// ActionForClaim maps a claim outcome to the runner's next action.
func ActionForClaim(o ClaimOutcome) RunnerAction {
	switch o.Kind {
	case OutcomeClaimed:
		return ActionExecute
	case OutcomeCached:
		return ActionAdoptCached
	case OutcomeRejected:
		if o.Reason == "deps_not_ready" {
			return ActionWaitDeps
		}
		return ActionSkip // job_held | terminal | not_found
	default:
		return ActionSkip
	}
}

// ActionForHeartbeat decides whether to keep working after a heartbeat: a lost
// lease (409 lease_lost) means another runner took the job, so stop.
func ActionForHeartbeat(leaseLost bool) RunnerAction {
	if leaseLost {
		return ActionStop
	}
	return ActionExecute
}

// ClaimableJobs is the local schedule: the fold's runnable frontier (jobs whose
// deps are all satisfied). The server's conditional :claim is the authority; the
// client only uses this to decide what to try.
func ClaimableJobs(state RunFoldState) []string {
	return state.Frontier
}
