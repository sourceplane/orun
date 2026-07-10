package platformmcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/remotestate"
)

// entFake wraps fakeAPI so the entitlements read can be steered (page/error)
// independently of the tool's own seam calls.
type entFake struct {
	fakeAPI
	entPage  *remotestate.PlatformPage
	entErr   error
	entCalls int
}

func (f *entFake) ListEntitlements(_ context.Context, org string) (*remotestate.PlatformPage, error) {
	f.entCalls++
	f.calls = append(f.calls, "ListEntitlements org="+org)
	return f.entPage, f.entErr
}

// TestEntitlementGate: grant on an explicit enabled row, deny on explicit
// enabled:false (as the platform's entitlement_required error, before any
// tool seam call), grant when the row is missing, and fail-open when the
// entitlements read itself fails.
func TestEntitlementGate(t *testing.T) {
	cases := []struct {
		name string
		page *remotestate.PlatformPage
		err  error
		deny bool
	}{
		{"explicit grant", page(`{"entitlements":[{"feature":"feature.mcp_server","enabled":true}]}`, ""), nil, false},
		{"explicit deny", page(`{"entitlements":[{"feature":"feature.mcp_server","enabled":false}]}`, ""), nil, true},
		{"deny via key field", page(`[{"key":"feature.mcp_server","enabled":false}]`, ""), nil, true},
		{"missing row grants", page(`{"entitlements":[{"feature":"feature.other","enabled":false}]}`, ""), nil, false},
		{"read failure fails open", nil, errors.New("billing backend down"), false},
	}
	for _, tc := range cases {
		api := &entFake{fakeAPI: fakeAPI{page: page(`{}`, "")}, entPage: tc.page, entErr: tc.err}
		p := &Provider{API: api}
		text, isErr := callTool(t, p, "projects_list", `{"workspace":"ws_1"}`)
		if api.entCalls != 1 {
			t.Errorf("%s: entitlements read %d times, want 1", tc.name, api.entCalls)
		}
		if tc.deny {
			if !isErr || !strings.Contains(text, "entitlement_required") {
				t.Errorf("%s: want entitlement_required verdict, got %q (isError=%v)", tc.name, text, isErr)
			}
			if len(api.calls) != 1 {
				t.Errorf("%s: a denied call reached the tool seam: %v", tc.name, api.calls)
			}
		} else if isErr {
			t.Errorf("%s: granted call errored: %s", tc.name, text)
		}
	}

	// Writes are gated by the same check.
	api := &entFake{entPage: page(`[{"feature":"feature.mcp_server","enabled":false}]`, "")}
	p := &Provider{API: api}
	text, isErr := callTool(t, p, "project_create", `{"workspace":"ws_1","name":"api"}`)
	if !isErr || !strings.Contains(text, "entitlement_required") {
		t.Fatalf("denied write: %q (isError=%v)", text, isErr)
	}
	if len(api.keys) != 0 {
		t.Fatal("a denied write reached the seam")
	}
}

// TestEntitlementCache: the verdict caches per workspace for 60s — a second
// call performs no second entitlements read; a new workspace does; expiry
// re-checks.
func TestEntitlementCache(t *testing.T) {
	api := &entFake{fakeAPI: fakeAPI{page: page(`{}`, "")}, entPage: page(`[]`, "")}
	p := &Provider{API: api}
	now := time.Now()
	p.ent.now = func() time.Time { return now }

	callTool(t, p, "projects_list", `{"workspace":"ws_1"}`)
	callTool(t, p, "projects_list", `{"workspace":"ws_1"}`)
	if api.entCalls != 1 {
		t.Fatalf("entitlements read %d times over two calls, want 1 (cached)", api.entCalls)
	}

	callTool(t, p, "projects_list", `{"workspace":"ws_2"}`)
	if api.entCalls != 2 {
		t.Fatalf("a new workspace must be checked: reads = %d, want 2", api.entCalls)
	}

	now = now.Add(entitlementTTL + time.Second)
	callTool(t, p, "projects_list", `{"workspace":"ws_1"}`)
	if api.entCalls != 3 {
		t.Fatalf("an expired verdict must be re-checked: reads = %d, want 3", api.entCalls)
	}

	// A cached denial also holds without a re-read.
	api = &entFake{entPage: page(`[{"feature":"feature.mcp_server","enabled":false}]`, "")}
	p = &Provider{API: api}
	callTool(t, p, "projects_list", `{"workspace":"ws_1"}`)
	if _, isErr := callTool(t, p, "projects_list", `{"workspace":"ws_1"}`); !isErr || api.entCalls != 1 {
		t.Fatalf("cached denial: isErr=%v reads=%d", isErr, api.entCalls)
	}
}
