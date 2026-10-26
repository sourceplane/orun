package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// seedDomainShadowingComponent turns the seeded workspace into the shape the
// turso-starter baseline had before its sweep: the svc-a component declares a
// domain of its own name, so the Domain and the Component share one ref.
func seedDomainShadowingComponent(t *testing.T) string {
	t.Helper()
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "svc-a", "component.yaml"), []byte(""+
		"apiVersion: orun.io/v1alpha1\n"+
		"kind: Component\n"+
		"metadata:\n"+
		"  name: svc-a\n"+
		"spec:\n"+
		"  type: service\n"+
		"  owner: team/x\n"+
		"  system: payments\n"+
		"  domain: svc-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "domain svc-a"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	resetCatalogFlags(t)
	return dir
}

// TestCatalogValidate_DomainNamedLikeAComponentIsAnError: validate refuses the
// catalog (exit 1), attributes the issue to the domain declaration, and its
// message names both entities and the fix.
func TestCatalogValidate_DomainNamedLikeAComponentIsAnError(t *testing.T) {
	seedDomainShadowingComponent(t)
	catalogJSONFlag = true

	out, err := captureStdoutErr(t, func() error { return runCatalogValidate(nil) })
	var coder interface{ ExitCode() int }
	if err == nil || !asExit(err, &coder) || coder.ExitCode() != 1 {
		t.Fatalf("validate = %v, want exit 1", err)
	}
	var env catalogEnvelope
	var data catalogValidateData
	env.Data = &data
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("validate envelope: %v\n%s", jerr, out)
	}
	var hit *catalogValidateIssue
	for i := range data.Issues {
		if data.Issues[i].Code == nodes.IssueCodeEntityRefCollision {
			hit = &data.Issues[i]
		}
	}
	if hit == nil {
		t.Fatalf("no %s issue in %+v", nodes.IssueCodeEntityRefCollision, data.Issues)
	}
	if hit.Severity != "error" || hit.File != "svc-a/component.yaml" || hit.Pointer != "/spec/domain" {
		t.Errorf("issue = %+v, want an error at svc-a/component.yaml#/spec/domain", hit)
	}
	for _, want := range []string{
		`Component "svc-a" (svc-a/component.yaml)`,
		`Domain "svc-a" (spec.domain in svc-a/component.yaml)`,
		"Rename one of them",
	} {
		if !strings.Contains(hit.Message, want) {
			t.Errorf("message missing %q:\n%s", want, hit.Message)
		}
	}
	if data.Valid || data.Errors == 0 || data.Issues[0].Severity != "error" {
		t.Errorf("data = %+v, want invalid with the error listed first", data)
	}
}

// TestCatalogValidate_CleanCatalogHasNoRefCollision: the seeded workspace
// (system payments, no clashing name) reports no collision.
func TestCatalogValidate_CleanCatalogHasNoRefCollision(t *testing.T) {
	refreshSeededCatalog(t)
	catalogJSONFlag = true
	t.Cleanup(func() { catalogJSONFlag = false })
	out := captureStdout(t, func() error { return runCatalogValidate(nil) })
	if strings.Contains(out, nodes.IssueCodeEntityRefCollision) {
		t.Errorf("clean catalog reports a ref collision:\n%s", out)
	}
}

// TestPlanWarnsAndKeepsTheComponent: the plan path prints the collision as a
// warning (the plan still succeeds) and the catalog it writes keeps the svc-a
// Component, leaving the same-named Domain out.
func TestPlanWarnsAndKeepsTheComponent(t *testing.T) {
	dir := seedDomainShadowingComponent(t)
	ctx := context.Background()

	catRes, err := resolvePlanCatalog(ctx, planCatalogOptions{})
	if err != nil || !catRes.Resolved {
		t.Fatalf("resolvePlanCatalog = %+v, %v", catRes, err)
	}

	orig := os.Stderr
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			b.Write(buf[:n])
			if rerr != nil {
				break
			}
		}
		done <- b.String()
	}()
	plan := &model.Plan{}
	writeObjectModelPlan(filepath.Join(dir, ".orun"), plan, []byte(`{"jobs":[]}`), "feedface00112233445566778899aabb", "rev-test",
		triggerctx.TriggerOccurrence{TriggerName: "system.manual", CreatedAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)}, catRes)
	_ = w.Close()
	os.Stderr = orig
	stderr := <-done

	if !strings.Contains(stderr, "warning: catalog: catalog ref ") ||
		!strings.Contains(stderr, `Component "svc-a" (svc-a/component.yaml)`) ||
		!strings.Contains(stderr, `Domain "svc-a" (spec.domain in svc-a/component.yaml)`) {
		t.Fatalf("plan stderr lacks the collision warning:\n%s", stderr)
	}

	root := objectModelRoot(filepath.Join(dir, ".orun"))
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(view.Components) != 1 || view.Components[0].Name != "svc-a" {
		t.Fatalf("components = %+v, want svc-a", view.Components)
	}
	key := view.Components[0].ComponentKey
	for _, e := range view.Entities {
		if e.EntityKey == key {
			t.Errorf("%s entity %s shares the component's ref; want it left out", e.Kind, e.EntityKey)
		}
	}
}
