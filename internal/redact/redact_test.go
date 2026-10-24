package redact

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
)

func TestFilterMasksRawValue(t *testing.T) {
	r := New()
	r.Add("hunter2-super-secret")
	got := r.Filter("db password is hunter2-super-secret ok")
	if strings.Contains(got, "hunter2-super-secret") {
		t.Fatalf("raw value survived: %q", got)
	}
	if !strings.Contains(got, Mask) {
		t.Fatalf("mask missing: %q", got)
	}
}

func TestFilterMasksEncodings(t *testing.T) {
	secret := "p@ss w0rd/with+special\"chars"
	r := New()
	r.Add(secret)

	b64 := base64.StdEncoding.EncodeToString([]byte(secret))
	if got := r.Filter("echoed: " + b64); strings.Contains(got, b64) {
		t.Errorf("base64 form survived: %q", got)
	}
	// URL-encoded form
	if got := r.Filter("url: p%40ss+w0rd%2Fwith%2Bspecial%22chars"); strings.Contains(got, "p%40ss") {
		t.Errorf("url-encoded form survived: %q", got)
	}
	// JSON-escaped form (as it would appear inside a JSON log line)
	if got := r.Filter(`{"pw":"p@ss w0rd/with+special\"chars"}`); strings.Contains(got, `special\"chars`) {
		t.Errorf("json-escaped form survived: %q", got)
	}
}

func TestFilterSkipsShortValues(t *testing.T) {
	r := New()
	r.Add("ab")
	if got := r.Filter("value ab here"); got != "value ab here" {
		t.Errorf("short values must not be masked (flooding): %q", got)
	}
}

func TestFilterNilAndEmptySafe(t *testing.T) {
	var nilR *Redactor
	if got := nilR.Filter("anything"); got != "anything" {
		t.Errorf("nil redactor must pass through, got %q", got)
	}
	if got := New().Filter("anything"); got != "anything" {
		t.Errorf("empty redactor must pass through, got %q", got)
	}
}

func TestConcurrentAddAndFilter(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			r.Add("secret-value-" + strings.Repeat("x", n+1))
		}(i)
		go func() {
			defer wg.Done()
			_ = r.Filter("log line with secret-value-x maybe")
		}()
	}
	wg.Wait()
	if got := r.Filter("secret-value-x"); got != Mask {
		t.Errorf("expected mask after concurrent adds, got %q", got)
	}
}

func TestLongestPatternWins(t *testing.T) {
	r := New()
	r.Add("abcd", "abcdefgh")
	if got := r.Filter("xx abcdefgh yy"); got != "xx "+Mask+" yy" {
		t.Errorf("longer value should mask whole, got %q", got)
	}
}

// saas-live-runs LR8: a stream redacted piece by piece is cut only where no
// secret spans the cut, so each piece masks exactly as the whole does.
func TestSafeCutNeverSplitsASecret(t *testing.T) {
	r := New()
	r.Add("s3cr3t-token", "-----BEGIN KEY-----\nabcd\n-----END KEY-----")
	if got := r.MaxLen(); got < len("-----BEGIN KEY-----\nabcd\n-----END KEY-----") {
		t.Fatalf("MaxLen = %d", got)
	}
	s := "line one\nauth s3cr3t-token here\n-----BEGIN KEY-----\nabcd\n-----END KEY-----\ntail\n"
	whole := r.Filter(s)
	for at := 0; at <= len(s); at++ {
		cut := r.SafeCut(s, at)
		if cut > at {
			t.Fatalf("SafeCut(%d) = %d moved forward", at, cut)
		}
		if got := r.Filter(s[:cut]) + r.Filter(s[cut:]); got != whole {
			t.Fatalf("cut at %d (asked %d): %q != %q", cut, at, got, whole)
		}
	}
	// Mid-secret asks back off to the secret's start.
	at := strings.Index(s, "s3cr3t") + 3
	if got, want := r.SafeCut(s, at), strings.Index(s, "s3cr3t"); got != want {
		t.Fatalf("SafeCut mid-secret = %d, want %d", got, want)
	}
}

func TestSafeCutKeepsRunesWholeAndIsNilSafe(t *testing.T) {
	var r *Redactor
	s := "héllo"
	if got := r.SafeCut(s, 2); got != 1 { // 'é' is bytes 1..2
		t.Fatalf("SafeCut mid-rune = %d, want 1", got)
	}
	if r.MaxLen() != 0 {
		t.Fatal("nil MaxLen")
	}
	if got := New().SafeCut(s, 99); got != len(s) {
		t.Fatalf("SafeCut past end = %d", got)
	}
}
