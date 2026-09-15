package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `orun baseline publish <id> <tag>` (orun-bootstrap-engine BE-O7b).
//
// The wire is asserted in `internal/remotestate/baselines_test.go`; what is
// left here is the one decision this verb makes before reaching the platform,
// and it is a decision because getting it wrong moves a registry pin.

// `id@tag` is the READ vocabulary — `show`, `check` and `new` all take it, and
// there the tag says WHICH VERSION TO LOOK AT. Here the tag is what CHANGES,
// so an `id@tag` first argument puts two tags in one command line and no
// reading of it is obviously right.
func TestPublishRefusesAnIDAtTagAddress(t *testing.T) {
	_, _, err := parsePublishArgs([]string{"cirrus@baseline-v5", "baseline-v6"})
	if err == nil {
		t.Fatal("accepted an id@tag address, which has two tags in it")
	}
	// The message has to say what to type instead. "invalid id" would leave
	// somebody guessing which half was wrong.
	if !strings.Contains(err.Error(), "two arguments") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
}

func TestPublishTakesAnIDAndATag(t *testing.T) {
	id, tag, err := parsePublishArgs([]string{" cirrus ", " baseline-v6 "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "cirrus" || tag != "baseline-v6" {
		t.Errorf("got %q %q", id, tag)
	}
}

// Whitespace is not a tag. A blank second argument would otherwise reach the
// door, which would refuse it — correctly, but one round trip and one
// confusing message later.
func TestPublishRefusesBlankArguments(t *testing.T) {
	for _, args := range [][]string{
		{"", "baseline-v6"},
		{"cirrus", ""},
		{"  ", "  "},
	} {
		if _, _, err := parsePublishArgs(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

// The verb has to be reachable: a command nobody registered is a command that
// does not exist, and every other test here would still pass.
func TestPublishIsRegisteredUnderBaseline(t *testing.T) {
	root := &cobra.Command{Use: "orun"}
	registerBaselineCommand(root)
	cmd, _, err := root.Find([]string{"baseline", "publish"})
	if err != nil || cmd.Name() != "publish" {
		t.Fatalf("`orun baseline publish` is not reachable: %v (%s)", err, cmd.Name())
	}
	if !strings.Contains(cmd.Use, "<id> <tag>") {
		t.Errorf("usage does not name both arguments: %q", cmd.Use)
	}
}
