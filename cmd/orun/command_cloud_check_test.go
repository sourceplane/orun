package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

func TestMatchRepoInLinks(t *testing.T) {
	links := []cliauth.WorkspaceLink{
		{OrgSlug: "acme", ProjectSlug: "infra", RemoteURL: "github.com/acme/infra"},
		{OrgSlug: "acme", ProjectSlug: "lumen", RemoteURL: "github.com/sourceplane/lumen"},
	}

	// Server canonical remote ends in owner/repo; raw repoFullName matches it
	// case-insensitively regardless of scheme/.git differences.
	repo := &repoContext{RepoFullName: "sourceplane/lumen"}
	if m := matchRepoInLinks(repo, links); m == nil || m.ProjectSlug != "lumen" {
		t.Fatalf("expected lumen match, got %+v", m)
	}

	// Different case still matches.
	repoUpper := &repoContext{RepoFullName: "SourcePlane/Lumen"}
	if m := matchRepoInLinks(repoUpper, links); m == nil || m.ProjectSlug != "lumen" {
		t.Fatalf("expected case-insensitive lumen match, got %+v", m)
	}

	// A repo not on the allow-list returns nil (caller must not over-claim).
	absent := &repoContext{RepoFullName: "sourceplane/other"}
	if m := matchRepoInLinks(absent, links); m != nil {
		t.Fatalf("expected no match for absent repo, got %+v", m)
	}
}

func TestResolveCheckOrgPrecedence(t *testing.T) {
	t.Setenv(orgEnvVar, "")
	// Flag wins.
	if got := resolveCheckOrg("org_flag", "org_intent", "org_link"); got != "org_flag" {
		t.Errorf("flag precedence: got %q", got)
	}
	// Env beats intent + link.
	t.Setenv(orgEnvVar, "org_env")
	if got := resolveCheckOrg("", "org_intent", "org_link"); got != "org_env" {
		t.Errorf("env precedence: got %q", got)
	}
	t.Setenv(orgEnvVar, "")
	// Intent beats link.
	if got := resolveCheckOrg("", "org_intent", "org_link"); got != "org_intent" {
		t.Errorf("intent precedence: got %q", got)
	}
	// Link is the floor.
	if got := resolveCheckOrg("", "", "org_link"); got != "org_link" {
		t.Errorf("link precedence: got %q", got)
	}
}
