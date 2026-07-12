package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/remotestate"
)

// TestDoctorPathMismatch: the field report's P0 — a stale `orun` earlier on
// PATH shadows the registered binary. Same file (by path or inode) → no
// warning; a different file → the actionable warning; no `orun` on PATH →
// the use-the-absolute-path note.
func TestDoctorPathMismatch(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "orun-self")
	other := filepath.Join(dir, "orun-other")
	for _, p := range []string{self, other} {
		if err := os.WriteFile(p, []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if warn := doctorPathMismatch(self, func(string) (string, error) { return self, nil }); warn != nil {
		t.Errorf("same path must not warn: %+v", warn)
	}

	// A hardlink is the same file even under a different name.
	link := filepath.Join(dir, "orun-link")
	if err := os.Link(self, link); err == nil {
		if warn := doctorPathMismatch(self, func(string) (string, error) { return link, nil }); warn != nil {
			t.Errorf("hardlink to the same file must not warn: %+v", warn)
		}
	}

	warn := doctorPathMismatch(self, func(string) (string, error) { return other, nil })
	if warn == nil {
		t.Fatal("a different binary on PATH must warn")
	}
	if !warn.warn || !warn.ok {
		t.Errorf("PATH mismatch is a warning, not a failure: %+v", warn)
	}
	if !strings.Contains(warn.line, other) || !strings.Contains(warn.line, "NOT this binary") {
		t.Errorf("warning must name the shadowing binary: %q", warn.line)
	}

	warn = doctorPathMismatch(self, func(string) (string, error) { return "", errors.New("not found") })
	if warn == nil || !strings.Contains(warn.line, "absolute path") {
		t.Errorf("missing `orun` on PATH must point at the absolute-path registration: %+v", warn)
	}
}

// TestClassifyDoctorProbe: backend-probe classification — 404/not_found on
// a platform route reads as "not an Orun Cloud API endpoint"; auth codes
// point at login; the same 404 on the work route is NOT over-claimed.
func TestClassifyDoctorProbe(t *testing.T) {
	if c := classifyDoctorProbe("platform API", "GET /v1/auth/profile", nil, true); !c.ok || !strings.HasSuffix(c.line, "→ ok") {
		t.Errorf("nil error must pass: %+v", c)
	}

	notFound := &remotestate.APIError{Code: "not_found", Message: "no such route", Status: 404}
	c := classifyDoctorProbe("platform API", "GET /v1/auth/profile", notFound, true)
	if c.ok || !strings.Contains(c.line, "not an Orun Cloud API endpoint") {
		t.Errorf("platform 404 must flag the wrong backend: %+v", c)
	}
	// The legacy backend's status-derived spelling classifies the same way.
	legacy := &remotestate.APIError{Code: "NOT_FOUND", Message: "server returned status 404", Status: 404}
	if c := classifyDoctorProbe("platform API", "GET /v1/auth/profile", legacy, true); !strings.Contains(c.line, "not an Orun Cloud API endpoint") {
		t.Errorf("legacy NOT_FOUND must flag the wrong backend: %+v", c)
	}
	// A work-route 404 is not over-claimed as a wrong backend.
	if c := classifyDoctorProbe("work API", "GET work summary", notFound, false); c.ok || strings.Contains(c.line, "not an Orun Cloud API endpoint") {
		t.Errorf("work 404 must not claim wrong-backend: %+v", c)
	}

	authErr := &remotestate.APIError{Code: "unauthorized", Message: "bad token", Status: 401}
	if c := classifyDoctorProbe("platform API", "GET /v1/auth/profile", authErr, true); c.ok || !strings.Contains(c.line, "orun auth login") {
		t.Errorf("auth rejection must point at login: %+v", c)
	}

	if c := classifyDoctorProbe("platform API", "GET /v1/auth/profile", fmt.Errorf("dial tcp: connection refused"), true); c.ok || !strings.Contains(c.line, "unreachable") {
		t.Errorf("a transport error must read as unreachable: %+v", c)
	}
}

// TestResolveDoctorWorkspace: the reported chain matches serve's precedence
// (flag > env > intent > link) and names the winning rung.
func TestResolveDoctorWorkspace(t *testing.T) {
	cases := []struct {
		flag, env, intent, link string
		wantVal, wantSrc        string
	}{
		{"ws_f", "ws_e", "ws_i", "ws_l", "ws_f", "--workspace"},
		{"", "ws_e", "ws_i", "ws_l", "ws_e", "ORUN_WORKSPACE/ORUN_ORG"},
		{"", "", "ws_i", "ws_l", "ws_i", "intent.yaml execution.state"},
		{"", "", "", "ws_l", "ws_l", "the cached repo link"},
		{"", "  ", "", "", "", ""},
	}
	for _, tc := range cases {
		val, src := resolveDoctorWorkspace(tc.flag, tc.env, tc.intent, tc.link)
		if val != tc.wantVal || src != tc.wantSrc {
			t.Errorf("resolveDoctorWorkspace(%q,%q,%q,%q) = (%q,%q), want (%q,%q)",
				tc.flag, tc.env, tc.intent, tc.link, val, src, tc.wantVal, tc.wantSrc)
		}
	}
}

// TestDescribeSessionAuth: expiry is reported without printing any token
// material; an expired access token with a live refresh is healthy; a fully
// expired session fails with the login line.
func TestDescribeSessionAuth(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	secret := "tok_SECRET_ACCESS"
	refSecret := "tok_SECRET_REFRESH"

	if ok, line := describeSessionAuth(nil, now); ok || !strings.Contains(line, "orun auth login") {
		t.Errorf("nil session: (%v, %q)", ok, line)
	}

	live := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(10 * time.Minute).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(24 * time.Hour).Format(time.RFC3339),
		User:               cliauth.SessionUser{Email: "dev@example.com"},
	}
	ok, line := describeSessionAuth(live, now)
	if !ok || !strings.Contains(line, "dev@example.com") || !strings.Contains(line, "valid until") {
		t.Errorf("live session: (%v, %q)", ok, line)
	}
	if strings.Contains(line, secret) || strings.Contains(line, refSecret) {
		t.Fatalf("session line leaks token material: %q", line)
	}

	refreshable := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	if ok, line := describeSessionAuth(refreshable, now); !ok || !strings.Contains(line, "refresh token live") {
		t.Errorf("refreshable session must pass: (%v, %q)", ok, line)
	}

	expired := &cliauth.Credentials{
		AccessToken:        secret,
		AccessTokenExpiry:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		RefreshToken:       refSecret,
		RefreshTokenExpiry: now.Add(-time.Hour).Format(time.RFC3339),
	}
	ok, line = describeSessionAuth(expired, now)
	if ok || !strings.Contains(line, "orun auth login") {
		t.Errorf("fully expired session must fail actionably: (%v, %q)", ok, line)
	}
	if strings.Contains(line, secret) || strings.Contains(line, refSecret) {
		t.Fatalf("expired-session line leaks token material: %q", line)
	}
}
