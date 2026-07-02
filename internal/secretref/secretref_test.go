package secretref

import "testing"

func TestParseValid(t *testing.T) {
	cases := []struct {
		in   string
		want Ref
	}{
		{
			in:   "secret://acme/api/prod/DATABASE_URL",
			want: Ref{Workspace: "acme", Project: "api", Env: "prod", Key: "DATABASE_URL"},
		},
		{
			in:   "secret://acme/api/prod/STRIPE_KEY@7",
			want: Ref{Workspace: "acme", Project: "api", Env: "prod", Key: "STRIPE_KEY", Version: 7},
		},
		{
			in:   "secret://acme-corp/my.repo/stage-2/a.b-c_d",
			want: Ref{Workspace: "acme-corp", Project: "my.repo", Env: "stage-2", Key: "a.b-c_d"},
		},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
		if got.String() != c.in {
			t.Errorf("Parse(%q).String() = %q, want round-trip", c.in, got.String())
		}
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"",
		"DATABASE_URL",                           // literal, no scheme
		"hunter2",                                // literal value
		"secret://acme/api/prod",                 // missing key
		"secret://acme/api/prod/KEY/extra",       // too many segments
		"secret://acme/api/prod/DATABASE_URL@0",  // version must be >= 1
		"secret://acme/api/prod/DATABASE_URL@x",  // non-numeric version
		"secret://acme/api/prod/1KEY",            // key must start with a letter
		"secret://acme/api/prod/",                // empty key
		"secret:///api/prod/KEY",                 // empty workspace
		"secret://acme/api/pr od/KEY",            // space in segment
		"secret://acme/api/prod/" + longKey(129), // key too long
	}
	for _, c := range cases {
		if _, err := Parse(c); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", c)
		}
	}
}

func TestIsRef(t *testing.T) {
	if !IsRef("secret://a/b/c/D") {
		t.Error("IsRef should accept scheme-prefixed strings")
	}
	if IsRef("hunter2") || IsRef("SECRET://a/b/c/D") {
		t.Error("IsRef must reject literals and case-mangled schemes")
	}
}

func longKey(n int) string {
	s := "K"
	for len(s) < n {
		s += "x"
	}
	return s
}
