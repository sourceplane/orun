package main

import (
	"strings"
	"testing"
)

func TestParseDotenvHappyPath(t *testing.T) {
	input := strings.Join([]string{
		"# comment",
		"",
		"DATABASE_URL=postgres://u:p@h/db",
		"export STRIPE_KEY=sk_live_abc",
		`QUOTED="with spaces"`,
		"SINGLE='single quoted'",
		"CRLF=windows\r",
		"EQ_IN_VALUE=a=b=c",
	}, "\n")

	entries, err := parseDotenv(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseDotenv: %v", err)
	}
	want := map[string]string{
		"DATABASE_URL": "postgres://u:p@h/db",
		"STRIPE_KEY":   "sk_live_abc",
		"QUOTED":       "with spaces",
		"SINGLE":       "single quoted",
		"CRLF":         "windows",
		"EQ_IN_VALUE":  "a=b=c",
	}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(entries), entries)
	}
	for _, e := range entries {
		if e.Invalid {
			t.Errorf("entry %s unexpectedly invalid: %s", e.Key, e.Reason)
			continue
		}
		if want[e.Key] != e.Value {
			t.Errorf("%s = %q, want %q", e.Key, e.Value, want[e.Key])
		}
	}
}

func TestParseDotenvInvalidEntriesDoNotFailBatch(t *testing.T) {
	input := strings.Join([]string{
		"GOOD=1234",
		"1BAD=starts-with-digit",
		"no-equals-line",
		"ALSO_GOOD=ok",
	}, "\n")

	entries, err := parseDotenv(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseDotenv: %v", err)
	}
	var good, invalid int
	for _, e := range entries {
		if e.Invalid {
			invalid++
			if strings.Contains(e.Reason, "starts-with-digit") {
				t.Errorf("invalid reason must never echo the value: %q", e.Reason)
			}
		} else {
			good++
		}
	}
	if good != 2 || invalid != 2 {
		t.Errorf("expected 2 good + 2 invalid, got %d + %d", good, invalid)
	}
}

func TestParseDotenvMismatchedQuotesKept(t *testing.T) {
	entries, err := parseDotenv(strings.NewReader(`K="half-open`))
	if err != nil {
		t.Fatalf("parseDotenv: %v", err)
	}
	if entries[0].Value != `"half-open` {
		t.Errorf("mismatched quotes must be preserved, got %q", entries[0].Value)
	}
}
