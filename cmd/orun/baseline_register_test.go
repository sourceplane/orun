package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `orun baseline register <id>` (orun-bootstrap-engine BE-O7b).
//
// The wire is asserted in `internal/remotestate/baselines_test.go`. What is
// left is the request this verb BUILDS, and two of its rules decide whether a
// registered row is buildable at all.

func ok() registerFlags {
	return registerFlags{
		name:       "Zephyr",
		sourceRepo: "acme/zephyr",
		tag:        "baseline-v1",
		minutes:    30,
		visibility: "private",
	}
}

// ── THE RULE THAT DECIDES WHETHER THE ROW IS BUILDABLE ─────────────────────
//
// A baseline with a brief is run through its umbrella; one with neither is
// BLUEPRINT-DRIVEN and its build document declares every phase. Half a shell
// layer is a row somebody edited halfway, and a build of it fetches a file
// that is not there.
func TestRegisterRefusesHalfAShellLayer(t *testing.T) {
	for _, f := range []registerFlags{
		func() registerFlags { x := ok(); x.brief = "flows/agent/BASELINE-TASK.md"; return x }(),
		func() registerFlags { x := ok(); x.umbrella = "flows/phases/00-all/workflow.yaml"; return x }(),
	} {
		_, err := registerRequest("zephyr", f)
		if err == nil {
			t.Fatalf("accepted half a shell layer: %+v", f)
		}
		if !strings.Contains(err.Error(), "a pair") {
			t.Errorf("the refusal does not say why: %v", err)
		}
	}
}

// Declaring NEITHER is the blueprint-driven shape, and the request must say so
// by sending neither field — not by sending two empty strings, which reads as
// a claim about paths the caller never mentioned.
func TestRegisterSendsNoShellPathsWhenThereIsNoShell(t *testing.T) {
	req, err := registerRequest("zephyr", ok())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.BriefPath != nil || req.UmbrellaPath != nil {
		t.Errorf("sent shell paths nobody declared: %v %v", req.BriefPath, req.UmbrellaPath)
	}
}

func TestRegisterCarriesABothShellLayer(t *testing.T) {
	f := ok()
	f.brief, f.umbrella = "flows/agent/BASELINE-TASK.md", "flows/phases/00-all/workflow.yaml"
	req, err := registerRequest("zephyr", f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.BriefPath == nil || *req.BriefPath != f.brief {
		t.Errorf("brief lost: %v", req.BriefPath)
	}
	if req.UmbrellaPath == nil || *req.UmbrellaPath != f.umbrella {
		t.Errorf("umbrella lost: %v", req.UmbrellaPath)
	}
}

// ── THE OTHER RULE, AND IT IS A SECURITY BOUNDARY ──────────────────────────
//
// A public baseline is a repository an agent clones into a stranger's
// workspace and runs guardrails from, so it is the platform's to grant. The
// door refuses it — that is the boundary — and this refuses it one round trip
// earlier, naming the flag the caller typed.
func TestRegisterRefusesPublic(t *testing.T) {
	f := ok()
	f.visibility = "public"
	_, err := registerRequest("zephyr", f)
	if err == nil {
		t.Fatal("accepted --visibility public")
	}
	if !strings.Contains(err.Error(), "granted by Orunbase") {
		t.Errorf("the refusal does not say whose it is: %v", err)
	}
}

func TestRegisterAcceptsUnlistedAndDefaultsToPrivate(t *testing.T) {
	f := ok()
	f.visibility = "unlisted"
	req, err := registerRequest("zephyr", f)
	if err != nil || req.Visibility != "unlisted" {
		t.Fatalf("unlisted refused: %v %+v", err, req)
	}
	// Private is the default, and is sent as ABSENT rather than as the word:
	// the door's own default is private, so saying it adds a field that can
	// only ever agree.
	f.visibility = "private"
	req, err = registerRequest("zephyr", f)
	if err != nil || req.Visibility != "" {
		t.Fatalf("private should ride as the default: %v %+v", err, req)
	}
}

// Every one of these is a fact that makes a build possible, and a row missing
// one is a row that fails in somebody's workspace. Named together so the
// caller fixes the command once rather than four times.
func TestRegisterNamesEveryMissingFlagAtOnce(t *testing.T) {
	_, err := registerRequest("zephyr", registerFlags{})
	if err == nil {
		t.Fatal("registered a baseline with nothing about it")
	}
	for _, flag := range []string{"--name", "--source-repo", "--tag", "--expected-minutes"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("%s not named: %v", flag, err)
		}
	}
}

func TestRegisterCarriesTheContractPath(t *testing.T) {
	f := ok()
	f.manifestPath = "blueprints/coolify.yaml"
	req, err := registerRequest("zephyr", f)
	if err != nil || req.ManifestPath != "blueprints/coolify.yaml" {
		t.Fatalf("contract path lost: %v %+v", err, req)
	}
}

func TestRegisterIsReachable(t *testing.T) {
	root := &cobra.Command{Use: "orun"}
	registerBaselineCommand(root)
	cmd, _, err := root.Find([]string{"baseline", "register"})
	if err != nil || cmd.Name() != "register" {
		t.Fatalf("`orun baseline register` is not reachable: %v", err)
	}
}
