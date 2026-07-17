package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
)

// Regression: the lease-bound secret resolve is addressed by the contract run
// ULID (state-worker's isRunUlid gate). The runner holds the CLI exec id, so
// remoteSecretResolver MUST map it through RunULID before the resolve call —
// exactly as every coordination verb does via wireRunID. Before the fix it sent
// the raw exec id and the backend rejected the path as "Route not found".
func TestRemoteSecretResolver_MapsExecIDToRunULID(t *testing.T) {
	const execID = "29470560319-1" // GitHub run_id-attempt, NOT a ULID
	wantRunID := remotestate.RunULID(execID)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"secrets":    map[string]any{"OGPIC_ORUN_SMOKE": "smoke-value"},
				"ttlSeconds": 300,
			},
			"meta": map[string]any{"requestId": "req_test"},
		})
		_, _ = io.Discard.Write(nil)
	}))
	defer srv.Close()

	client := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("t"))
	// r=nil (provenance skipped for empty resolved), backend=nil (epoch 0).
	resolve := remoteSecretResolver(context.Background(), nil, client, nil, execID, "runner-1", os.Stderr, false)

	out, err := resolve("orun-secrets-tests.dev.verify", []model.PlanSecretRef{
		{Ref: "secret://sourceplane/ogpic/dev/OGPIC_ORUN_SMOKE", AsEnv: "OGPIC_ORUN_SMOKE"},
	})
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}
	if out["OGPIC_ORUN_SMOKE"] != "smoke-value" {
		t.Errorf("expected injected value under AsEnv, got %v", out)
	}
	if !strings.HasSuffix(gotPath, "/runs/"+wantRunID+"/secrets/resolve") {
		t.Errorf("resolve path must use the wire ULID %q; got %q", wantRunID, gotPath)
	}
	if strings.Contains(gotPath, execID) {
		t.Errorf("raw exec id %q must not appear in the ULID-gated resolve path %q", execID, gotPath)
	}
}
