package main

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// Which repository a platform-run build writes into (BE-O7b).
//
// This chooses where an hour of automated commits lands — branches created,
// pull requests merged, terraform applied against a real cloud account — so
// the only unacceptable behaviour is guessing.

func link(name, id string, over ...string) remotestate.RepoLink {
	l := remotestate.RepoLink{ID: id, RepoFullName: name, Status: "active", AgentAccess: "write"}
	if len(over) > 0 {
		l.AgentAccess = over[0]
	}
	return l
}

func TestRepoLinkResolvesTheOneMatch(t *testing.T) {
	got, err := resolveRepoLink([]remotestate.RepoLink{
		link("acme/other", "repl_1"),
		link("acme/storefront", "repl_2"),
	}, "acme/storefront")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "repl_2" {
		t.Errorf("resolved %q", got.ID)
	}
}

// GitHub resolves `Acme/Storefront` and `acme/storefront` to the same place,
// and orun-cloud's build lease is keyed lowercased for exactly that reason. A
// case-sensitive match here would send somebody to link a repository that is
// already linked.
func TestRepoLinkMatchesTheWayGitHubDoes(t *testing.T) {
	got, err := resolveRepoLink([]remotestate.RepoLink{link("Acme/Storefront", "repl_2")}, "acme/storefront")
	if err != nil || got.ID != "repl_2" {
		t.Fatalf("case-sensitive match: %v %+v", err, got)
	}
}

// ── THE ONE THING THIS MUST NEVER DO ───────────────────────────────────────
//
// A workspace can hold two links to one repository. Picking "the first" is not
// a reason, and the cost of being wrong is an hour of commits in a repository
// nobody chose.
func TestRepoLinkRefusesToPickBetweenTwoLinks(t *testing.T) {
	_, err := resolveRepoLink([]remotestate.RepoLink{
		link("acme/storefront", "repl_a"),
		link("acme/storefront", "repl_b"),
	}, "acme/storefront")
	if err == nil {
		t.Fatal("picked between two links to the same repository")
	}
	// Both ids, so the next command can be typed from this message.
	for _, want := range []string{"repl_a", "repl_b", "--repo-link"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// Agent access is a ceiling, and `off` means this repository is linked for
// everything EXCEPT this. The door refuses it too; saying it here costs no
// round trip and names the switch to flip.
func TestRepoLinkRefusesAgentAccessOff(t *testing.T) {
	_, err := resolveRepoLink([]remotestate.RepoLink{link("acme/storefront", "repl_2", "off")}, "acme/storefront")
	if err == nil {
		t.Fatal("accepted a repository an agent may not write to")
	}
	if !strings.Contains(err.Error(), "Git tab") {
		t.Errorf("the refusal does not say where to fix it: %v", err)
	}
}

// "No link for acme/storefront" plus silence sends somebody to the console to
// read a list this command already has in hand.
func TestRepoLinkNamesWhatIsLinkedInstead(t *testing.T) {
	_, err := resolveRepoLink([]remotestate.RepoLink{
		link("acme/other", "repl_1"),
		link("acme/third", "repl_3"),
	}, "acme/storefront")
	if err == nil {
		t.Fatal("resolved a repository that is not linked")
	}
	for _, want := range []string{"acme/other", "acme/third"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("did not name %q: %v", want, err)
		}
	}
}

func TestRepoLinkSaysWhenNothingIsLinkedAtAll(t *testing.T) {
	_, err := resolveRepoLink(nil, "acme/storefront")
	if err == nil {
		t.Fatal("resolved against an empty workspace")
	}
	if !strings.Contains(err.Error(), "no linked repositories at all") {
		t.Errorf("unhelpful: %v", err)
	}
}

func TestRepoLinkRefusesAnEmptyName(t *testing.T) {
	if _, err := resolveRepoLink([]remotestate.RepoLink{link("acme/storefront", "repl_2")}, "  "); err == nil {
		t.Fatal("resolved with no repository named")
	}
}
