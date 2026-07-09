package catalogmodel

import (
	"strings"
	"testing"
)

// docs.go landed with CD1 (#463) untested and dropped the package below its
// 90% coverage gate; these tests pin its derivation rules.

func TestIsWellKnownDocRole(t *testing.T) {
	for _, role := range []string{"guide", "architecture", "runbook", "adr", "reference", "changelog", "faq", "onboarding"} {
		if !IsWellKnownDocRole(role) {
			t.Errorf("well-known role %q not recognized", role)
		}
	}
	for _, role := range []string{"", "Guide", "how-to", "postmortem"} {
		if IsWellKnownDocRole(role) {
			t.Errorf("role %q should not be well-known (free taxonomy renders neutrally)", role)
		}
	}
}

func TestIsDocSlug(t *testing.T) {
	for _, ok := range []string{"overview", "a", "0day", "release-notes", "adr-0001"} {
		if !IsDocSlug(ok) {
			t.Errorf("valid slug %q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-lead", "Upper", "has space", "under_score", "dot.md"} {
		if IsDocSlug(bad) {
			t.Errorf("invalid slug %q accepted", bad)
		}
	}
}

func TestDocKeyFromPath(t *testing.T) {
	cases := map[string]string{
		"docs/Release Notes.md":    "release-notes",
		"docs/ADR_0001 (draft).md": "adr-0001-draft",
		"guide.md":                 "guide",
		"docs/2026-07-plan.md":     "2026-07-plan",
		"docs/---.md":              "", // nothing usable — author must name it
		"docs/nested/path/FAQ.md":  "faq",
		"docs/trailing-.md":        "trailing",
	}
	for in, want := range cases {
		if got := DocKeyFromPath(in); got != want {
			t.Errorf("DocKeyFromPath(%q) = %q, want %q", in, got, want)
		}
	}
	// The cap trims to MaxDocKeyLen without a trailing hyphen.
	long := strings.Repeat("ab-", 40) + "tail.md"
	got := DocKeyFromPath("docs/" + long)
	if len(got) > MaxDocKeyLen || strings.HasSuffix(got, "-") {
		t.Errorf("capped key %q exceeds %d or ends with hyphen", got, MaxDocKeyLen)
	}
}

func TestDocTitleFromContent(t *testing.T) {
	// First ATX H1 wins, trimmed.
	if got := DocTitleFromContent([]byte("\n<!-- lead comment -->\n# The Title  \nbody"), "x/y.md"); got != "The Title" {
		t.Errorf("H1 title = %q", got)
	}
	// A long H1 is capped at 120.
	long := "# " + strings.Repeat("t", 200)
	if got := DocTitleFromContent([]byte(long), "x/y.md"); len(got) > 120 {
		t.Errorf("capped title still %d chars", len(got))
	}
	// First real content line not an H1 → filename stem title-cased.
	if got := DocTitleFromContent([]byte("plain text first"), "docs/release-notes.md"); got != "Release Notes" {
		t.Errorf("fallback title = %q", got)
	}
	// Empty content → filename fallback too.
	if got := DocTitleFromContent(nil, "docs/faq_and_answers.md"); got != "Faq And Answers" {
		t.Errorf("empty-content title = %q", got)
	}
}

func TestResolvedDocAttached(t *testing.T) {
	if (ResolvedDoc{}).Attached() {
		t.Error("declared-only doc reports attached")
	}
	if !(ResolvedDoc{Bytes: []byte("x")}).Attached() {
		t.Error("doc with bytes reports detached")
	}
}
