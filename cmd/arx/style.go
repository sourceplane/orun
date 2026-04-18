package main

import (
	"os"

	"github.com/sourceplane/arx/internal/ui"
)

var cliColorEnabled = ui.ColorEnabledForWriter(os.Stdout)

func stylePanel(s string) string {
	return ui.Cyan(cliColorEnabled, s)
}

func styleTitle(s string) string {
	return ui.BoldCyan(cliColorEnabled, s)
}

func styleTip(s string) string {
	return ui.Dim(cliColorEnabled, s)
}

func styleOK(s string) string {
	return ui.Green(cliColorEnabled, s)
}

func styleWarn(s string) string {
	return ui.Yellow(cliColorEnabled, s)
}
