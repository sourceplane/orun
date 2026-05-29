package triggerctx

import (
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

type fakeGit struct {
	src TriggerSource
	err error
}

func (f fakeGit) TriggerSource() (TriggerSource, error) { return f.src, f.err }

func TestResolveTriggerContext_AllBranches(t *testing.T) {
	t.Parallel()

	intent := minimalIntent()
	headSrc := TriggerSource{HeadRevision: "def456a1b2c3", WorkingTree: WorkingTreeClean}
	event := &model.NormalizedEvent{
		Provider: "github", Event: "pull_request", Action: "opened",
		HeadSHA: "deadbeef1234567", BaseSHA: "cafef00d7654321",
	}

	cases := []struct {
		name    string
		opts    ResolveOptions
		check   func(t *testing.T, occ TriggerOccurrence, err error)
		intent  *model.Intent
		gitSrc  GitSource
	}{
		{
			name: "system/manual",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemManual, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemManual {
					t.Fatalf("got %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/manual-changed",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemManualChanged, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.PlanScope.Mode != PlanScopeChanged {
					t.Fatalf("expected changed plan scope, got %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/default-flavor-is-manual",
			opts: ResolveOptions{Kind: ResolveKindSystem, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemManual {
					t.Fatalf("default flavor: %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/api",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemAPI, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemAPI {
					t.Fatalf("api: %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/migrated",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemMigrated, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemMigrated {
					t.Fatalf("migrated: %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/replay-via-flavor",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemReplay, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemReplay {
					t.Fatalf("replay flavor: %+v err=%v", occ, err)
				}
			},
		},
		{
			name: "system/unknown-flavor-errors",
			opts: ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: "bogus", Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err == nil {
					t.Fatal("expected error for unknown system flavor")
				}
			},
		},
		{
			name: "replay/kind",
			opts: ResolveOptions{Kind: ResolveKindReplay, Source: headSrc},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != SystemReplay || occ.Mode != ModeReplay {
					t.Fatalf("replay: %+v err=%v", occ, err)
				}
			},
		},
		{
			name:   "declared-by-name",
			intent: intent,
			opts: ResolveOptions{
				Kind: ResolveKindDeclaredByName, TriggerName: "github-pull-request",
				Source: TriggerSource{SourceScope: "pr-9", HeadRevision: "def456a1b2c3"},
				Action: "opened",
			},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil {
					t.Fatalf("err: %v", err)
				}
				if occ.TriggerName != "github-pull-request" || occ.TriggerType != TriggerTypeDeclared {
					t.Fatalf("declared mismatch: %+v", occ)
				}
			},
		},
		{
			name:   "declared-by-name/unknown",
			intent: intent,
			opts:   ResolveOptions{Kind: ResolveKindDeclaredByName, TriggerName: "nope"},
			check: func(t *testing.T, _ TriggerOccurrence, err error) {
				if err == nil {
					t.Fatal("expected unknown-binding error")
				}
				if errors.Is(err, ErrNoMatchingBinding) {
					t.Errorf("declared-by-name unknown leaked --from-ci sentinel")
				}
			},
		},
		{
			name:   "from-ci/match",
			intent: intent,
			opts:   ResolveOptions{Kind: ResolveKindFromCI, ProviderEvent: event, Source: TriggerSource{SourceScope: "pr-9"}},
			check: func(t *testing.T, occ TriggerOccurrence, err error) {
				if err != nil || occ.TriggerName != "github-pull-request" {
					t.Fatalf("from-ci match: %+v err=%v", occ, err)
				}
			},
		},
		{
			name:   "from-ci/no-match-typed-error",
			intent: intent,
			opts: ResolveOptions{Kind: ResolveKindFromCI, ProviderEvent: &model.NormalizedEvent{Provider: "gitlab", Event: "merge_request"}},
			check: func(t *testing.T, _ TriggerOccurrence, err error) {
				if !errors.Is(err, ErrNoMatchingBinding) {
					t.Fatalf("expected ErrNoMatchingBinding, got %v", err)
				}
				var typed *NoMatchingBindingError
				if !errors.As(err, &typed) {
					t.Fatalf("expected typed error, got %T", err)
				}
			},
		},
		{
			name: "unknown-kind",
			opts: ResolveOptions{Kind: ResolveKind(99)},
			check: func(t *testing.T, _ TriggerOccurrence, err error) {
				if err == nil {
					t.Fatal("expected error for unknown kind")
				}
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			occ, err := ResolveTriggerContext(c.opts, c.intent, c.gitSrc)
			c.check(t, occ, err)
		})
	}
}

func TestResolveTriggerContext_GitProbeMerge(t *testing.T) {
	t.Parallel()
	git := fakeGit{src: TriggerSource{
		Repo: "owner/repo", Ref: "refs/heads/main", HeadRevision: "abcdef0123456",
		WorkingTree: WorkingTreeClean, SourceScope: "branch-main",
	}}
	occ, err := ResolveTriggerContext(ResolveOptions{Kind: ResolveKindSystem, SystemFlavor: SystemManual}, nil, git)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// SourceScope from git should propagate (no override supplied).
	if occ.Source.SourceScope != "branch-main" {
		t.Errorf("git scope not used: %+v", occ.Source)
	}
	if occ.Source.HeadRevision != "abcdef0123456" {
		t.Errorf("git head not used: %+v", occ.Source)
	}
}

func TestResolveTriggerContext_OverrideTakesPrecedence(t *testing.T) {
	t.Parallel()
	git := fakeGit{src: TriggerSource{Repo: "git/repo", HeadRevision: "111111111", WorkingTree: WorkingTreeClean}}
	occ, err := ResolveTriggerContext(ResolveOptions{
		Kind: ResolveKindSystem, SystemFlavor: SystemManual,
		Source: TriggerSource{Repo: "override/repo"},
	}, nil, git)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if occ.Source.Repo != "override/repo" {
		t.Errorf("override.Repo did not win: %+v", occ.Source)
	}
	if occ.Source.HeadRevision != "111111111" {
		t.Errorf("non-overridden git field lost: %+v", occ.Source)
	}
}

func TestResolveTriggerContext_GitErrorIsSoft(t *testing.T) {
	t.Parallel()
	git := fakeGit{err: errors.New("not a git repo")}
	occ, err := ResolveTriggerContext(ResolveOptions{
		Kind: ResolveKindSystem, SystemFlavor: SystemManual,
		Source: TriggerSource{HeadRevision: "abcdef0"},
	}, nil, git)
	if err != nil {
		t.Fatalf("expected git error to be soft: %v", err)
	}
	if occ.Source.HeadRevision != "abcdef0" {
		t.Errorf("Source fell through to override: %+v", occ.Source)
	}
}
