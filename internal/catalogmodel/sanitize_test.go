package catalogmodel_test

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestSanitizeBranch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"main", "main"},
		{"feature/foo-bar", "feature-foo-bar"},
		{"FEATURE/Foo_Bar", "feature-foo-bar"},
		{"---", ""},
		{"a", "a"},
		{strings.Repeat("a", 40), strings.Repeat("a", 40)},
		// 41 'a's: truncated path. Prefix becomes 31 chars (all 'a'), then
		// '-' + 8 hex of sha256("aaaa...a"). The hex is deterministic so we
		// only assert the shape here and check len.
	}
	for _, tc := range cases {
		got := catalogmodel.SanitizeBranch(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeBranch(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
	// Long-input branch.
	long := strings.Repeat("longbranch-", 10) // 110 chars, well over 40.
	out := catalogmodel.SanitizeBranch(long)
	if len(out) != 40 {
		t.Errorf("long branch sanitization: got len=%d, want 40 (out=%q)", len(out), out)
	}
	// Output is fully truncated form: 31 chars prefix + '-' + 8 hex.
	if out[31] != '-' {
		t.Errorf("expected '-' at pos 31 in %q", out)
	}
}

func TestSanitizeComponentKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a/b/c", "a-b-c"},
		{"sourceplane/orun/api-edge", "sourceplane-orun-api-edge"},
		{"x", "x"},
	}
	for _, tc := range cases {
		got := catalogmodel.SanitizeComponentKey(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeComponentKey(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeEventKind(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"execution.completed", "execution-completed"},
		{"a.b.c", "a-b-c"},
		{"plain", "plain"},
	}
	for _, tc := range cases {
		got := catalogmodel.SanitizeEventKind(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeEventKind(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestShortHex(t *testing.T) {
	cases := []struct {
		full string
		n    int
		want string
	}{
		{"abc123def456", 6, "abc123"},
		{"ABC123", 6, "abc123"},
		{"  abc123  ", 6, "abc123"},
		{"xyz", 6, ""},   // non-hex
		{"abc", 4, ""},   // too short
		{"abc", 0, ""},   // n==0
		{"abc", -1, ""},  // negative n
		{"", 4, ""},      // empty
	}
	for _, tc := range cases {
		got := catalogmodel.ShortHex(tc.full, tc.n)
		if got != tc.want {
			t.Errorf("ShortHex(%q,%d) = %q; want %q", tc.full, tc.n, got, tc.want)
		}
	}
}
