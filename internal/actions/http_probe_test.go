package actions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func probe(t *testing.T, with map[string]any) (Result, error) {
	t.Helper()
	return Run(context.Background(), "orun.http/probe@v1", Input{Params: with})
}

func TestProbeSucceedsWhenEveryURLAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res, err := probe(t, map[string]any{"urls": []any{srv.URL, srv.URL + "/health"}})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Outputs["checked"] != "2" {
		t.Errorf("checked should be 2, got %q", res.Outputs["checked"])
	}
	if res.Outputs["failures"] != "0" {
		t.Errorf("failures should be 0, got %q", res.Outputs["failures"])
	}
}

func TestProbeReportsEveryFailingURLNotJustTheFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res, err := probe(t, map[string]any{"urls": []any{srv.URL + "/ok", srv.URL + "/a", srv.URL + "/b"}})
	if err == nil {
		t.Fatal("expected an error when a URL answers the wrong status")
	}
	// An operator fixing a broken deploy needs the whole list, not the first
	// thing that went wrong.
	if !strings.Contains(err.Error(), "/a") || !strings.Contains(err.Error(), "/b") {
		t.Errorf("error should name every failing URL; got %v", err)
	}
	if res.Outputs["failures"] != "2" {
		t.Errorf("failures should be 2, got %q", res.Outputs["failures"])
	}
}

func TestProbeHonoursExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if _, err := probe(t, map[string]any{"urls": []any{srv.URL}, "expectStatus": 204}); err != nil {
		t.Fatalf("204 was expected and returned, so this should pass: %v", err)
	}
	if _, err := probe(t, map[string]any{"urls": []any{srv.URL}}); err == nil {
		t.Fatal("default expectStatus is 200, so a 204 must fail")
	}
}

// A verify step that accepts a redirect is asserting that something answered,
// not that the right thing did.
func TestProbeDoesNotFollowRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer final.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	if _, err := probe(t, map[string]any{"urls": []any{redirector.URL}}); err == nil {
		t.Fatal("a 301 must not be accepted as a 200")
	}
}

func TestProbeRejectsNonAbsoluteURL(t *testing.T) {
	_, err := probe(t, map[string]any{"urls": []any{"example.test/health"}})
	if err == nil {
		t.Fatal("expected an error for a URL with no scheme")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should say what is wrong with it; got %v", err)
	}
}

func TestProbeRejectsEmptyList(t *testing.T) {
	if _, err := probe(t, map[string]any{"urls": []any{}}); err == nil {
		t.Fatal("an empty url list verifies nothing and must not report success")
	}
}
