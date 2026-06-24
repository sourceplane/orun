package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

// TestMaterializePersonalOrg verifies the UO2 zero-org path: with no orgs, the
// CLI creates a personal org named/slugged after the session handle and returns
// it for linking.
func TestMaterializePersonalOrg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := cliauth.SaveSession(&cliauth.Credentials{GitHubLogin: "rahul"}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	var gotName, gotSlug string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/organizations" {
			var b struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			gotName, gotSlug = b.Name, b.Slug
			linkData(w, 201, map[string]interface{}{
				"organization": map[string]string{"id": "org_new", "name": b.Name, "slug": b.Slug},
			})
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	org, err := materializePersonalOrg(context.Background(), client, "tok")
	if err != nil {
		t.Fatalf("materializePersonalOrg() error = %v", err)
	}
	if org.ID != "org_new" {
		t.Errorf("org.ID = %q, want org_new", org.ID)
	}
	if gotName != "rahul" || gotSlug != "rahul" {
		t.Errorf("sent name/slug = %q/%q, want rahul/rahul", gotName, gotSlug)
	}
}

// TestMaterializePersonalOrgRetriesOnConflict verifies a slug collision (409)
// is retried once with a suffixed slug.
func TestMaterializePersonalOrgRetriesOnConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := cliauth.SaveSession(&cliauth.Credentials{GitHubLogin: "rahul"}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	var attempts int
	var secondSlug string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/organizations" {
			attempts++
			var b struct {
				Slug string `json:"slug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if attempts == 1 {
				linkErr(w, 409, "conflict", "Organization already exists", nil)
				return
			}
			secondSlug = b.Slug
			linkData(w, 201, map[string]interface{}{
				"organization": map[string]string{"id": "org_2", "name": "rahul", "slug": b.Slug},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	org, err := materializePersonalOrg(context.Background(), client, "tok")
	if err != nil {
		t.Fatalf("materializePersonalOrg() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (retry on conflict)", attempts)
	}
	if org.ID != "org_2" {
		t.Errorf("org.ID = %q, want org_2", org.ID)
	}
	if !strings.HasPrefix(secondSlug, "rahul-") {
		t.Errorf("retry slug = %q, want a rahul- prefixed suffix", secondSlug)
	}
}

func TestPersonalOrgIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cases := []struct {
		name     string
		creds    cliauth.Credentials
		wantName string
		wantSlug string
	}{
		{"github login", cliauth.Credentials{GitHubLogin: "Rahul-V"}, "Rahul-V", "rahul-v"},
		{"email local-part", cliauth.Credentials{User: cliauth.SessionUser{Email: "rahul.varghese@sourceplane.ai"}}, "rahul.varghese", "rahul-varghese"},
		{"display name", cliauth.Credentials{User: cliauth.SessionUser{DisplayName: "Rahul"}}, "Rahul", "rahul"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = cliauth.ClearSession()
			if err := cliauth.SaveSession(&tc.creds); err != nil {
				t.Fatalf("SaveSession() error = %v", err)
			}
			name, slug := personalOrgIdentity()
			if name != tc.wantName || slug != tc.wantSlug {
				t.Errorf("personalOrgIdentity() = (%q, %q), want (%q, %q)", name, slug, tc.wantName, tc.wantSlug)
			}
		})
	}
}

func TestSlugifyOrg(t *testing.T) {
	cases := map[string]string{
		"Rahul-V":        "rahul-v",
		"rahul.varghese": "rahul-varghese",
		"  Acme  Inc ":   "acme-inc",
		"AB":             "ab",
	}
	for in, want := range cases {
		if got := slugifyOrg(in); got != want {
			t.Errorf("slugifyOrg(%q) = %q, want %q", in, got, want)
		}
	}
	// Too-short input falls back to a random "org-…" slug (≥2 chars, valid).
	if got := slugifyOrg("a"); !strings.HasPrefix(got, "org-") || len(got) < 2 {
		t.Errorf("slugifyOrg(%q) = %q, want an org- fallback", "a", got)
	}
}
