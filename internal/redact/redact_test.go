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
