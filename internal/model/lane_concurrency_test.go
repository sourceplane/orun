package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLaneConcurrency_YAML(t *testing.T) {
	var cfg IntentLanes
	if err := yaml.Unmarshal([]byte("concurrency: auto\ngroups:\n  fleet: { concurrency: auto }\n  big: { concurrency: 8 }\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Concurrency.IsAuto() || !cfg.Groups["fleet"].Concurrency.IsAuto() || cfg.Groups["big"].Concurrency != 8 {
		t.Fatalf("parsed %+v", cfg)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "concurrency: auto") || !strings.Contains(string(out), "concurrency: 8") {
		t.Fatalf("round trip:\n%s", out)
	}

	if err := yaml.Unmarshal([]byte("concurrency: 6\n"), &cfg); err != nil || cfg.Concurrency != 6 {
		t.Fatalf("a count: %v %+v", err, cfg)
	}
	if err := yaml.Unmarshal([]byte("concurrency: many\n"), &cfg); err == nil || !strings.Contains(err.Error(), `"auto"`) {
		t.Fatalf("a word other than auto must be refused, got %v", err)
	}

	// Unset stays out of the document.
	out, _ = yaml.Marshal(IntentLanes{Mode: "auto"})
	if strings.Contains(string(out), "concurrency") {
		t.Fatalf("zero must be omitted:\n%s", out)
	}
}

func TestLaneConcurrency_JSON(t *testing.T) {
	cfg := IntentLanes{Mode: "auto", Concurrency: LaneConcurrencyAuto, Groups: map[string]LaneGroup{"g": {Concurrency: 3}}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"concurrency":"auto"`) || !strings.Contains(string(b), `"concurrency":3`) {
		t.Fatalf("json = %s", b)
	}
	var back IntentLanes
	if err := json.Unmarshal(b, &back); err != nil || !back.Concurrency.IsAuto() || back.Groups["g"].Concurrency != 3 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	if b, _ := json.Marshal(IntentLanes{Mode: "auto"}); strings.Contains(string(b), "concurrency") {
		t.Fatalf("zero must be omitted: %s", b)
	}
}

func TestStep_ValidateScope(t *testing.T) {
	if err := (Step{Name: "x", Run: "pnpm install"}).ValidateScope(); err != nil {
		t.Fatal(err)
	}
	if err := (Step{Name: "x", Run: "pnpm install", Scope: "lane"}).ValidateScope(); err != nil {
		t.Fatal(err)
	}
	if err := (Step{Name: "x", Use: "actions/setup-node@v4", Scope: "lane"}).ValidateScope(); err == nil || !strings.Contains(err.Error(), "run steps only") {
		t.Fatalf("a use step cannot be lane-scoped: %v", err)
	}
	if err := (Step{Name: "x", Run: "true", Scope: "fleet"}).ValidateScope(); err == nil || !strings.Contains(err.Error(), "job|lane") {
		t.Fatalf("an unknown scope is refused: %v", err)
	}
}
