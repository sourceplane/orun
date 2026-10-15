package runner

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// SB1 — terraform HTTP state-backend env: a wired provider stamps TF_HTTP_*
// into every step's JobEnv for the job's (component, environment); no provider
// (local runs) or no component means no TF_HTTP_* vars at all.

func tfJob(component, environment string) model.PlanJob {
	return model.PlanJob{
		ID:          component + "." + environment + ".job",
		UID:         "uid-1",
		Component:   component,
		Environment: environment,
	}
}

func TestStepExecContextInjectsTfBackendEnv(t *testing.T) {
	r := &Runner{ExecID: "exec-1"}
	var gotComponent, gotEnv string
	r.TfBackendEnv = func(_ context.Context, component, environment string) map[string]string {
		gotComponent, gotEnv = component, environment
		return map[string]string{
			"TF_HTTP_ADDRESS":  "https://api.example.com/v1/organizations/org_1/projects/prj_1/state/tfstate/" + component + "/" + environment,
			"TF_HTTP_USERNAME": "orun",
			"TF_HTTP_PASSWORD": "tok-123",
		}
	}

	ctx, cancel, err := r.stepExecContext(executor.ExecContext{}, tfJob("cloudflare-kv", "stage"), model.PlanStep{}, ".", nil)
	defer cancel()
	if err != nil {
		t.Fatalf("stepExecContext: %v", err)
	}
	if gotComponent != "cloudflare-kv" || gotEnv != "stage" {
		t.Fatalf("provider called with (%q, %q), want (cloudflare-kv, stage)", gotComponent, gotEnv)
	}
	if got := ctx.Env["TF_HTTP_ADDRESS"]; got != "https://api.example.com/v1/organizations/org_1/projects/prj_1/state/tfstate/cloudflare-kv/stage" {
		t.Fatalf("TF_HTTP_ADDRESS = %q", got)
	}
	if ctx.Env["TF_HTTP_PASSWORD"] != "tok-123" || ctx.Env["TF_HTTP_USERNAME"] != "orun" {
		t.Fatalf("credentials not injected: %q / %q", ctx.Env["TF_HTTP_USERNAME"], ctx.Env["TF_HTTP_PASSWORD"])
	}
	// Runtime IDs still present — the tf env merges on top, not instead.
	if ctx.Env["ORUN_COMPONENT"] != "cloudflare-kv" {
		t.Fatalf("ORUN_COMPONENT lost: %q", ctx.Env["ORUN_COMPONENT"])
	}
}

func TestStepExecContextNoTfEnvWithoutProviderOrComponent(t *testing.T) {
	// No provider wired (local run).
	r := &Runner{ExecID: "exec-1"}
	ctx, cancel, err := r.stepExecContext(executor.ExecContext{}, tfJob("cloudflare-kv", "stage"), model.PlanStep{}, ".", nil)
	defer cancel()
	if err != nil {
		t.Fatalf("stepExecContext: %v", err)
	}
	if _, ok := ctx.Env["TF_HTTP_ADDRESS"]; ok {
		t.Fatal("TF_HTTP_ADDRESS injected without a provider")
	}

	// Provider wired but the job carries no component (a bare workflow step):
	// the provider must not even be called.
	r2 := &Runner{ExecID: "exec-1"}
	called := false
	r2.TfBackendEnv = func(context.Context, string, string) map[string]string {
		called = true
		return map[string]string{"TF_HTTP_ADDRESS": "x"}
	}
	ctx2, cancel2, err := r2.stepExecContext(executor.ExecContext{}, model.PlanJob{ID: "j", UID: "u"}, model.PlanStep{}, ".", nil)
	defer cancel2()
	if err != nil {
		t.Fatalf("stepExecContext: %v", err)
	}
	if called {
		t.Fatal("provider called for a job with no component/environment")
	}
	if _, ok := ctx2.Env["TF_HTTP_ADDRESS"]; ok {
		t.Fatal("TF_HTTP_ADDRESS injected for a componentless job")
	}
}

// A step's relative `working-directory` is applied once, by the caller that
// resolves the step's directory; stepExecContext takes that directory as is.
// Applied twice, `../..` from apps/x reached the parent of the repository.
func TestStepExecContextKeepsTheResolvedWorkingDir(t *testing.T) {
	r := &Runner{}
	resolved := r.resolveStepWorkingDir("/repo/apps/x", "../..")
	if resolved != "/repo" {
		t.Fatalf("resolved = %q, want /repo", resolved)
	}
	ctx, cancel, err := r.stepExecContext(executor.ExecContext{}, model.PlanJob{ID: "j", UID: "u"}, model.PlanStep{WorkingDirectory: "../.."}, resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if ctx.WorkDir != "/repo" {
		t.Fatalf("WorkDir = %q, want /repo (applied once)", ctx.WorkDir)
	}
}
