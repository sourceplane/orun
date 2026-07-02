package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/materialize"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/redact"
)

// recordingAdapter records every Put so a test can assert delivery.
type recordingAdapter struct {
	puts []struct {
		script, key, value string
	}
	err error
}

func (*recordingAdapter) Name() string { return "cloudflare-worker" }
func (a *recordingAdapter) Put(_ context.Context, target materialize.TargetBinding, key, value string) error {
	a.puts = append(a.puts, struct{ script, key, value string }{target.ScriptName, key, value})
	return a.err
}

func materializePlan(secrets []string) *model.Plan {
	return &model.Plan{Execution: model.PlanExecution{FailFast: true}, Jobs: []model.PlanJob{{
		ID:          "api@deploy",
		Name:        "api",
		Component:   "api",
		Environment: "prod",
		SecretRefs: []model.PlanSecretRef{
			{AsEnv: "DATABASE_URL", Ref: "secret://acme/api/prod/DATABASE_URL"},
			{AsEnv: "STRIPE_KEY", Ref: "secret://acme/api/prod/STRIPE_KEY"},
		},
		Materialize: &model.PlanMaterialize{Target: "cloudflare-worker", Secrets: secrets},
		Steps:       []model.PlanStep{{ID: "deploy", Name: "deploy"}},
	}}}
}

func materializeResolver(r *Runner) func(string, []model.PlanSecretRef) (map[string]string, error) {
	return func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
		r.RecordSecretProvenance(jobID, []execmodel.SecretResolution{
			{Key: "DATABASE_URL", Version: 9, Scope: "environment", DecisionID: "dec_1"},
			{Key: "STRIPE_KEY", Version: 3, Scope: "workspace", DecisionID: "dec_2"},
		})
		return map[string]string{
			"DATABASE_URL": "postgres://db-secret",
			"STRIPE_KEY":   "sk_live_secret",
		}, nil
	}
}

func TestMaterialize_DeliversResolvedValuesAndRecordsSync(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.ExecID = "run_abc"
	r.Redactor = redact.New()

	adapter := &recordingAdapter{}
	reg := materialize.NewRegistry()
	reg.Register(adapter)
	r.MaterializeAdapters = reg

	var records []materialize.SyncRecord
	r.MaterializeSyncRecorder = func(_ context.Context, rec materialize.SyncRecord) error {
		records = append(records, rec)
		return nil
	}

	r.Hooks = &RunnerHooks{ResolveJobSecrets: materializeResolver(r)}

	if err := r.Run(materializePlan([]string{"DATABASE_URL", "STRIPE_KEY"})); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(adapter.puts) != 2 {
		t.Fatalf("expected 2 Put calls, got %d: %+v", len(adapter.puts), adapter.puts)
	}
	byKey := map[string]string{}
	for _, p := range adapter.puts {
		byKey[p.key] = p.value
		if p.script != "api" { // derived from the component name (TODO: provisioned entity)
			t.Errorf("script name should default to the component, got %q", p.script)
		}
	}
	if byKey["DATABASE_URL"] != "postgres://db-secret" || byKey["STRIPE_KEY"] != "sk_live_secret" {
		t.Errorf("adapter did not receive the resolved values: %+v", byKey)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 sync records, got %d", len(records))
	}
	recByKey := map[string]materialize.SyncRecord{}
	for _, rec := range records {
		recByKey[rec.SecretKey] = rec
	}
	if recByKey["DATABASE_URL"].Version != 9 || recByKey["STRIPE_KEY"].Version != 3 {
		t.Errorf("sync records carry the wrong versions: %+v", recByKey)
	}
	for _, rec := range records {
		if rec.Target != "cloudflare-worker" || rec.RunID != "run_abc" {
			t.Errorf("sync record missing target/runId: %+v", rec)
		}
		// Value-free: the resolved value must never appear in a sync record.
		blob, _ := json.Marshal(rec)
		if strings.Contains(string(blob), "secret") && strings.Contains(string(blob), "sk_live") {
			t.Errorf("sync record leaked a value: %s", blob)
		}
	}
}

func TestMaterialize_MissingResolvedKeyFails(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()

	adapter := &recordingAdapter{}
	reg := materialize.NewRegistry()
	reg.Register(adapter)
	r.MaterializeAdapters = reg
	r.Hooks = &RunnerHooks{ResolveJobSecrets: materializeResolver(r)}

	// SESSION_KEY is listed to materialize but never resolved for this job.
	err := r.Run(materializePlan([]string{"DATABASE_URL", "SESSION_KEY"}))
	if err == nil {
		t.Fatal("expected the run to fail when a materialize key was not resolved")
	}
	snap := r.SnapshotState()
	if js := snap.Jobs["api@deploy"]; js == nil || js.Status != "failed" {
		t.Fatalf("job should have sealed failed, got %+v", js)
	}
}

func TestMaterialize_PutErrorSealsRed(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()

	adapter := &recordingAdapter{err: errors.New("cloudflare 500")}
	reg := materialize.NewRegistry()
	reg.Register(adapter)
	r.MaterializeAdapters = reg
	r.Hooks = &RunnerHooks{ResolveJobSecrets: materializeResolver(r)}

	err := r.Run(materializePlan([]string{"DATABASE_URL"}))
	if err == nil {
		t.Fatal("a Put error must fail the materialize step and seal the run red")
	}
	snap := r.SnapshotState()
	if js := snap.Jobs["api@deploy"]; js == nil || js.Status != "failed" {
		t.Fatalf("job should have sealed failed on Put error, got %+v", js)
	}
	if !strings.Contains(snap.Jobs["api@deploy"].LastError, "materialize") {
		t.Errorf("failure should be attributed to materialize, got %q", snap.Jobs["api@deploy"].LastError)
	}
}

func TestMaterialize_NoAdaptersConfiguredFails(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()
	// No MaterializeAdapters wired, but a job declares a materialize block.
	r.Hooks = &RunnerHooks{ResolveJobSecrets: materializeResolver(r)}

	if err := r.Run(materializePlan([]string{"DATABASE_URL"})); err == nil {
		t.Fatal("a materialize job with no adapters configured must fail loudly")
	}
}
