package materialize

import (
	"context"
	"errors"
	"testing"
)

// fakeAdapter records every Put call so a test can assert delivery.
type fakeAdapter struct {
	name string
	puts []putCall
	err  error
}

type putCall struct {
	target TargetBinding
	key    string
	value  string
}

func (a *fakeAdapter) Name() string { return a.name }
func (a *fakeAdapter) Put(_ context.Context, target TargetBinding, key, value string) error {
	a.puts = append(a.puts, putCall{target: target, key: key, value: value})
	return a.err
}

func TestRegistryLookupAndPut(t *testing.T) {
	fa := &fakeAdapter{name: "cloudflare-worker"}
	reg := NewRegistry()
	reg.Register(fa)

	got, ok := reg.Lookup("cloudflare-worker")
	if !ok {
		t.Fatal("expected adapter to be registered")
	}
	if err := got.Put(context.Background(), TargetBinding{ScriptName: "api"}, "DATABASE_URL", "secret-value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(fa.puts) != 1 {
		t.Fatalf("expected 1 Put, got %d", len(fa.puts))
	}
	if fa.puts[0].key != "DATABASE_URL" || fa.puts[0].value != "secret-value" || fa.puts[0].target.ScriptName != "api" {
		t.Errorf("unexpected Put: %+v", fa.puts[0])
	}
}

func TestRegistryUnknownTarget(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.Lookup("aws-ssm"); ok {
		t.Fatal("unregistered target must not resolve")
	}
	err := reg.UnknownTargetError("aws-ssm")
	if err == nil {
		t.Fatal("expected an error naming the unknown target")
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var reg *Registry
	if _, ok := reg.Lookup("cloudflare-worker"); ok {
		t.Fatal("nil registry must report not-found")
	}
	reg.Register(&fakeAdapter{name: "x"}) // must not panic
}

// fakeWorkerClient captures SetWorkerSecret calls without touching Cloudflare.
type fakeWorkerClient struct {
	script string
	name   string
	value  string
	calls  int
	err    error
}

func (c *fakeWorkerClient) SetWorkerSecret(_ context.Context, scriptName, secretName, secretValue string) error {
	c.calls++
	c.script = scriptName
	c.name = secretName
	c.value = secretValue
	return c.err
}

func TestCloudflareWorkerAdapter_CallsSetWorkerSecret(t *testing.T) {
	fc := &fakeWorkerClient{}
	a := NewCloudflareWorkerAdapter(fc)
	if a.Name() != CloudflareWorkerTarget {
		t.Errorf("name: %q", a.Name())
	}
	err := a.Put(context.Background(), TargetBinding{ScriptName: "payments-api"}, "STRIPE_KEY", "sk_live_xyz")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fc.calls != 1 || fc.script != "payments-api" || fc.name != "STRIPE_KEY" || fc.value != "sk_live_xyz" {
		t.Errorf("SetWorkerSecret called wrong: script=%q name=%q calls=%d", fc.script, fc.name, fc.calls)
	}
}

func TestCloudflareWorkerAdapter_RequiresScriptName(t *testing.T) {
	a := NewCloudflareWorkerAdapter(&fakeWorkerClient{})
	if err := a.Put(context.Background(), TargetBinding{}, "K", "v"); err == nil {
		t.Fatal("expected error when no Worker script name is derived")
	}
}

func TestCloudflareWorkerAdapter_PropagatesError(t *testing.T) {
	fc := &fakeWorkerClient{err: errors.New("cf 500")}
	a := NewCloudflareWorkerAdapter(fc)
	if err := a.Put(context.Background(), TargetBinding{ScriptName: "api"}, "K", "v"); err == nil {
		t.Fatal("expected the client error to propagate")
	}
}
