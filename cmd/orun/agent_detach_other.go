//go:build !unix

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func detachBody(cmd *cobra.Command, sessionID string) error {
	return fmt.Errorf("--detach is not supported on this platform yet; run in a terminal multiplexer instead")
}
