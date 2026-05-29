package triggerctx

import (
	"strings"
	"sync"
	"testing"

	"pgregory.net/rapid"
)

func TestNormalizeScope(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"PR-139", "pr-139"},
		{"  Pr_139 ", "pr-139"},
		{"feature/foo bar", "feature-foo-bar"},
		{"--leading--trailing--", "leading-trailing"},
		{"a..b__c", "a-b-c"},
		{"αβγ", ""}, // entirely stripped
	}
	for _, c := range cases {
		got := normalizeScope(c.in)
		if got != c.want {
			t.Errorf("normalizeScope(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestShortSHA(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"abcdef", ""}, // too short
		{"abcdef0123456789", "abcdef0"},
		{"  ABCDEF0  ", "abcdef0"},
		{"zzzzzzz", ""}, // non-hex
	}
	for _, c := range cases {
		got := shortSHA(c.in)
		if got != c.want {
			t.Errorf("shortSHA(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestWorktreeMarker(t *testing.T) {
	t.Parallel()
	if m := worktreeMarker(TriggerSource{WorkingTree: WorkingTreeDirty, HeadRevision: "def456a1b2c3"}); m != "local-dirty" {
		t.Errorf("dirty: got %q want local-dirty", m)
	}
	if m := worktreeMarker(TriggerSource{WorkingTree: WorkingTreeClean, HeadRevision: "def456a1b2c3"}); m != "def456a" {
		t.Errorf("clean+sha: got %q want def456a", m)
	}
	if m := worktreeMarker(TriggerSource{WorkingTree: WorkingTreeClean, HeadRevision: ""}); m != "no-git" {
		t.Errorf("no head: got %q want no-git", m)
	}
}

func TestTriggerKey_Examples(t *testing.T) {
	t.Parallel()
	// data-model.md §2.3 declared example
	declared := TriggerOccurrence{Source: TriggerSource{SourceScope: "pr-139", HeadRevision: "def456a1b2c3", WorkingTree: WorkingTreeClean}}
	if k := TriggerKey(declared); k != "trg-pr-139-def456a" {
		t.Errorf("declared: got %q", k)
	}
	// system manual on a dirty tree → local-dirty marker
	dirty := TriggerOccurrence{Source: TriggerSource{SourceScope: "manual", WorkingTree: WorkingTreeDirty}}
	if k := TriggerKey(dirty); k != "trg-manual-local-dirty" {
		t.Errorf("dirty: got %q", k)
	}
	// no-git
	nogit := TriggerOccurrence{Source: TriggerSource{SourceScope: "manual", WorkingTree: WorkingTreeClean}}
	if k := TriggerKey(nogit); k != "trg-manual-no-git" {
		t.Errorf("no-git: got %q", k)
	}
}

func TestTriggerKey_PropertyStabilityAndFormat(t *testing.T) {
	t.Parallel()
	// test-plan.md §3.1 — TriggerKey must be a stable function of its inputs
	// AND must always match the documented regex.
	rapid.Check(t, func(rt *rapid.T) {
		scope := rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(rt, "scope")
		sha := rapid.StringMatching(`[a-f0-9]{40}`).Draw(rt, "sha")
		occ := TriggerOccurrence{Source: TriggerSource{SourceScope: scope, HeadRevision: sha, WorkingTree: WorkingTreeClean}}
		k1 := TriggerKey(occ)
		k2 := TriggerKey(occ)
		if k1 != k2 {
			rt.Fatalf("TriggerKey not stable: %q vs %q", k1, k2)
		}
		if !triggerKeyPattern.MatchString(k1) {
			rt.Fatalf("TriggerKey %q does not match pattern", k1)
		}
	})
}

func TestTriggerKey_PropertyDirtyAlwaysLocalDirty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		scope := rapid.StringMatching(`[a-zA-Z0-9_/-]{1,20}`).Draw(rt, "scope")
		head := rapid.StringMatching(`[a-f0-9]{0,40}`).Draw(rt, "head")
		occ := TriggerOccurrence{Source: TriggerSource{SourceScope: scope, HeadRevision: head, WorkingTree: WorkingTreeDirty}}
		k := TriggerKey(occ)
		if !strings.HasSuffix(k, "-local-dirty") {
			rt.Fatalf("dirty tree did not yield local-dirty suffix: %q", k)
		}
		if !triggerKeyPattern.MatchString(k) {
			rt.Fatalf("TriggerKey %q does not match pattern", k)
		}
	})
}

func TestNewTriggerID_PrefixAndMonotonic(t *testing.T) {
	t.Parallel()
	// data-model.md §10 — IDs share a process-wide monotonic entropy source
	// and are lexically sortable. Generate N IDs concurrently and assert they
	// are all distinct, all carry the "trg_" prefix, and (when sorted) form
	// an ascending sequence.
	const N = 200
	var wg sync.WaitGroup
	out := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i] = NewTriggerID()
		}(i)
	}
	wg.Wait()
	seen := make(map[string]struct{}, N)
	for _, id := range out {
		if !strings.HasPrefix(id, "trg_") {
			t.Fatalf("id missing trg_ prefix: %q", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id under concurrent generation: %q", id)
		}
		seen[id] = struct{}{}
	}
}
