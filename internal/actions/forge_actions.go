package actions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/forge"
)

// orun.repo/ensure@v1 and orun.run/watch@v1 (orun-bootstrap-engine BE-O5c).

func init() {
	register(Spec{
		ID:      "orun.repo/ensure@v1",
		Summary: "Find or create the product repository",
		Doing:   "Finding the product repository.",
		Params: []Param{
			{Name: "owner", Type: ParamString, Required: true,
				Description: "GitHub owner (organization or user)"},
			{Name: "name", Type: ParamString, Required: true,
				Description: "repository name"},
			{Name: "private", Type: ParamBool, Default: true,
				Description: "create it private"},
			{Name: "inOrg", Type: ParamBool, Default: true,
				Description: "owner is an organization; false creates in the token's own account"},
		},
		Outputs: []string{"fullName", "cloneUrl", "created", "remote"},
	}, runRepoEnsure)

	register(Spec{
		ID:      "orun.run/watch@v1",
		Summary: "Watch a convergence run to green, resuming it through transient failures",
		Doing:   "Watching the convergence run on main — this is where it deploys.",
		Params: []Param{
			{Name: "repo", Type: ParamString, Required: true,
				Description: "owner/name of the repository whose run is watched"},
			{Name: "branch", Type: ParamString, Default: "main",
				Description: "branch whose newest run is watched"},
			{Name: "runId", Type: ParamInt, Default: 0,
				Description: "watch this run instead of the branch's newest"},
			{Name: "sha", Type: ParamString,
				Description: "watch the run for this commit — e.g. the landing's mergeSha — waiting for it to appear"},
			{Name: "waitSeconds", Type: ParamInt, Default: 0,
				Description: "how long to watch; 0 reports pending as soon as it is not finished"},
			{Name: "pollSeconds", Type: ParamInt, Default: 40,
				Description: "how often to re-read the run"},
			{Name: "resumeBudget", Type: ParamInt, Default: 3,
				Description: "how many times a failed run may be re-opened before it is called a regression"},
		},
		Outputs: []string{"runId", "conclusion", "url", "resumes"},
	}, runRunWatch)
}

func forgeClient(ctx context.Context) (*forge.Client, error) {
	token := githubToken(ctx)
	if token == "" {
		return nil, fmt.Errorf("a GitHub credential is required (GITHUB_TOKEN / GH_TOKEN / gh auth, or a platform session grounded on the repository)")
	}
	return &forge.Client{Token: token, TokenFn: func() string { return githubToken(ctx) }}, nil
}

func runRepoEnsure(ctx context.Context, in Input) (Result, error) {
	client, err := forgeClient(ctx)
	if err != nil {
		return Result{}, err
	}
	owner, name := StringParam(in, "owner"), StringParam(in, "name")
	res, err := client.EnsureRepo(ctx, owner, name, BoolParam(in, "private"), BoolParam(in, "inOrg"))
	if err != nil {
		return Result{}, err
	}
	remote, err := wireOrigin(ctx, in.Dir, owner, name)
	if err != nil {
		return Result{}, fmt.Errorf("the repository exists, but wiring it as origin failed: %w", err)
	}
	return Result{Outputs: map[string]string{
		"fullName": res.FullName, "cloneUrl": res.CloneURL, "created": fmt.Sprint(res.Created), "remote": remote,
	}}, nil
}

// watcher is the slice of the forge these actions use, so the resume logic —
// the part with a budget and a judgement in it — is testable without GitHub.
type watcher interface {
	LatestRun(ctx context.Context, owner, repo, branch string) (*forge.Run, error)
	RunForCommit(ctx context.Context, owner, repo, branch, sha string) (*forge.Run, error)
	GetRun(ctx context.Context, owner, repo string, id int64) (*forge.Run, error)
	RerunFailed(ctx context.Context, owner, repo string, id int64) error
	FailedJobs(ctx context.Context, owner, repo string, id int64) ([]string, error)
}

func runRunWatch(ctx context.Context, in Input) (Result, error) {
	client, err := forgeClient(ctx)
	if err != nil {
		return Result{}, err
	}
	return watchOn(ctx, client, in, time.Sleep)
}

// watchOn watches a convergence, resuming it through transient failures.
//
// # Why this belongs in the binary rather than in a baseline's shell
//
// A baseline polls GitHub's Actions API to ask about an execution ORUN owns,
// then calls `orun run --retry` to resume it. The binary knows which lanes are
// its own and which are retriable; GitHub's `conclusion` field knows neither.
// Moving the loop here is the one case in this epic where the code gets better
// rather than merely shorter.
//
// # Why a failure may be resumed, and why only so many times
//
// The CI is resume-capable: re-opening failed lanes re-runs exactly those, with
// memoized jobs staying done. So a convergence that trips on something
// transient — propagation, a rate-limited resolve, an evicted runner — heals in
// place. A real regression fails every resume and surfaces after the budget,
// which is what the budget is FOR: "flake" is not a root cause, and a loop with
// no bound would retry a genuine break forever.
func watchOn(ctx context.Context, c watcher, in Input, sleep func(time.Duration)) (Result, error) {
	full := StringParam(in, "repo")
	owner, repo, ok := strings.Cut(full, "/")
	if !ok || owner == "" || repo == "" {
		return Result{}, fmt.Errorf("repo must be owner/name, got %q", full)
	}
	branch := StringParam(in, "branch")
	budget := IntParam(in, "resumeBudget")
	poll := time.Duration(IntParam(in, "pollSeconds")) * time.Second
	if poll <= 0 {
		poll = 40 * time.Second
	}
	deadline := time.Now().Add(time.Duration(IntParam(in, "waitSeconds")) * time.Second)

	var run *forge.Run
	var err error
	sha := strings.TrimSpace(StringParam(in, "sha"))
	switch id := int64(IntParam(in, "runId")); {
	case id > 0:
		run, err = c.GetRun(ctx, owner, repo, id)
	case sha != "":
		// THE RUN FOR THE COMMIT THAT WAS LANDED, not the branch's newest.
		//
		// A convergence is watched the moment its landing merges, and GitHub
		// registers the push's run a few seconds later. In that gap the
		// branch's newest run is the PREVIOUS phase's — already green — or,
		// after the very first landing, there is none at all, which reads as
		// "nothing to converge". Either way the watch answered "converged" for
		// a run that had not started, and the next phase went ahead of it.
		//
		// So a named commit is waited for until its run exists: absent is
		// "not yet", reported pending once the wait runs out, never success.
		for {
			if run, err = c.RunForCommit(ctx, owner, repo, branch, sha); err != nil || run != nil {
				break
			}
			if time.Now().After(deadline) {
				return Result{
					Outputs: map[string]string{"runId": "0", "conclusion": "", "url": "", "resumes": "0"},
					Pending: &Pending{Reason: fmt.Sprintf("no run on %s for %s yet", branch, sha), RetryAfter: poll},
				}, nil
			}
			sleep(poll)
		}
	default:
		run, err = c.LatestRun(ctx, owner, repo, branch)
	}
	if err != nil {
		return Result{}, err
	}
	if run == nil {
		// No run at all is a real answer: a repository whose CI has not landed
		// yet has nothing to converge, and waiting for one would hang the first
		// phase of every bootstrap.
		return Result{Outputs: map[string]string{
			"runId": "0", "conclusion": "none", "url": "", "resumes": "0",
		}}, nil
	}

	resumes := 0
	for {
		current, err := c.GetRun(ctx, owner, repo, run.ID)
		if err != nil {
			return Result{}, err
		}
		out := map[string]string{
			"runId": fmt.Sprint(current.ID), "conclusion": current.Conclusion,
			"url": current.HTMLURL, "resumes": fmt.Sprint(resumes),
		}
		if current.Status == "completed" {
			switch current.Conclusion {
			case "success":
				return Result{Outputs: out}, nil
			default:
				if resumes >= budget {
					failed, _ := c.FailedJobs(ctx, owner, repo, current.ID)
					msg := fmt.Sprintf("convergence %s after %d resume(s) (run %d)", current.Conclusion, resumes, current.ID)
					if len(failed) > 0 {
						msg += ": " + strings.Join(failed, ", ")
					}
					return Result{Outputs: out}, fmt.Errorf("%s", msg)
				}
				resumes++
				if rerr := c.RerunFailed(ctx, owner, repo, current.ID); rerr != nil {
					// The resume is best-effort; the DIAGNOSIS is not. Losing
					// the failed-lane list because the retry could not be
					// issued is what costs someone an afternoon.
					failed, _ := c.FailedJobs(ctx, owner, repo, current.ID)
					msg := fmt.Sprintf("convergence %s (run %d) and the resume could not be issued: %v", current.Conclusion, current.ID, rerr)
					if len(failed) > 0 {
						msg += "; failed lanes: " + strings.Join(failed, ", ")
					}
					return Result{Outputs: out}, fmt.Errorf("%s", msg)
				}
			}
		}
		if time.Now().After(deadline) {
			out["conclusion"] = current.Conclusion
			return Result{Outputs: out, Pending: &Pending{
				Reason:     fmt.Sprintf("convergence run %d is %s", current.ID, current.Status),
				RetryAfter: poll,
			}}, nil
		}
		sleep(poll)
	}
}
