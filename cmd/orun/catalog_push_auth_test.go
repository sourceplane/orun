package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

// fakePushTokenSource is a remotestate.TokenSource that returns a fixed result,
// standing in for a session/static source without touching the credential store.
type fakePushTokenSource struct {
	token string
	err   error
}

func (f fakePushTokenSource) Token(context.Context) (string, error) { return f.token, f.err }

func TestValidatePushToken(t *testing.T) {
	ctx := context.Background()

	t.Run("valid token passes", func(t *testing.T) {
		if err := validatePushToken(ctx, fakePushTokenSource{token: "ok"}); err != nil {
			t.Fatalf("validatePushToken = %v, want nil", err)
		}
	})

	// A missing session file (os.ErrNotExist) and a revoked family
	// (ErrSessionRevoked) are both "no usable login" — they must map to the
	// single actionable login message, not a raw token error.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"missing session file", os.ErrNotExist},
		{"wrapped missing file", fmt.Errorf("load session: %w", os.ErrNotExist)},
		{"revoked session", cliauth.ErrSessionRevoked},
	} {
		t.Run(tc.name+" maps to not-logged-in", func(t *testing.T) {
			err := validatePushToken(ctx, fakePushTokenSource{err: tc.err})
			if err == nil {
				t.Fatalf("validatePushToken = nil, want not-logged-in error")
			}
			if got := err.Error(); !strings.Contains(got, "auth login") {
				t.Fatalf("error = %q, want the `orun auth login` hint", got)
			}
			// It must not leak the low-level token error to the user.
			if strings.Contains(err.Error(), "file does not exist") {
				t.Fatalf("error leaked the raw token failure: %q", err.Error())
			}
		})
	}

	t.Run("other auth error is wrapped, not swallowed", func(t *testing.T) {
		sentinel := errors.New("connection refused")
		err := validatePushToken(ctx, fakePushTokenSource{err: sentinel})
		if err == nil {
			t.Fatal("validatePushToken = nil, want wrapped error")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want it to wrap %v", err, sentinel)
		}
		if !strings.Contains(err.Error(), "remote state auth") {
			t.Fatalf("error = %q, want a 'remote state auth' prefix", err.Error())
		}
	})
}
