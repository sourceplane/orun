package cliauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinkRepoFromSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/accounts/repos/link" {
			http.Error(w, "unexpected request", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer cli-session-token" {
			http.Error(w, "missing auth", 401)
			return
		}
		var body struct {
			RepoFullName string `json:"repoFullName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RepoFullName == "" {
			http.Error(w, "bad body", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"namespaceId":   "ns-abc",
			"namespaceSlug": body.RepoFullName,
			"linkedAt":      "2026-05-07T10:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	resp, err := c.LinkRepoFromSession(context.Background(), "cli-session-token", "sourceplane/orun")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NamespaceID != "ns-abc" {
		t.Errorf("NamespaceID = %q, want ns-abc", resp.NamespaceID)
	}
	if resp.NamespaceSlug != "sourceplane/orun" {
		t.Errorf("NamespaceSlug = %q, want sourceplane/orun", resp.NamespaceSlug)
	}
}

func TestLinkRepoFromSession_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "repo slug not found; re-run orun auth login",
			"code":  "NOT_FOUND",
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	_, err := c.LinkRepoFromSession(context.Background(), "cli-token", "unknown/repo")
	if err == nil {
		t.Fatal("expected error for NOT_FOUND")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", apiErr.Code)
	}
}

func TestLinkRepoFromSession_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "namespace not in session allowedNamespaceIds",
			"code":  "FORBIDDEN",
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	_, err := c.LinkRepoFromSession(context.Background(), "cli-token", "private/repo")
	if err == nil {
		t.Fatal("expected error for FORBIDDEN")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != "FORBIDDEN" {
		t.Errorf("Code = %q, want FORBIDDEN", apiErr.Code)
	}
}

func TestLinkRepoFromSession_Idempotent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"namespaceId":   "ns-abc",
			"namespaceSlug": "sourceplane/orun",
			"linkedAt":      "2026-05-07T10:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	for i := 0; i < 3; i++ {
		resp, err := c.LinkRepoFromSession(context.Background(), "tok", "sourceplane/orun")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if resp.NamespaceID != "ns-abc" {
			t.Errorf("call %d: NamespaceID = %q", i, resp.NamespaceID)
		}
	}
	if calls != 3 {
		t.Errorf("expected 3 server calls, got %d", calls)
	}
}
