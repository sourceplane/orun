package main

import (
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/ui"
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

func styleStatus(status string, color bool) string {
	switch strings.ToLower(status) {
	case "completed":
		return ui.Green(color, "✓")
	case "failed":
		return ui.Red(color, "✗")
	case "running":
		return ui.Blue(color, "●")
	case "pending":
		return ui.Dim(color, "○")
	default:
		return ui.Dim(color, "?")
	}
}

func styleStatusText(status string, color bool) string {
	switch strings.ToLower(status) {
	case "completed":
		return ui.Green(color, status)
	case "failed":
		return ui.Red(color, status)
	case "running":
		return ui.Blue(color, status)
	case "pending":
		return ui.Dim(color, status)
	default:
		return ui.Dim(color, status)
	}
}

func shortenJobName(name, compositionType string) string {
	if compositionType == "" {
		return name
	}
	suffix := "-" + strings.ToLower(strings.ReplaceAll(compositionType, " ", "-"))
	shortened := strings.TrimSuffix(name, suffix)
	if shortened != name {
		return shortened
	}
	parts := strings.Split(strings.ToLower(compositionType), "-")
	for i := len(parts); i > 0; i-- {
		candidate := "-" + strings.Join(parts[:i], "-")
		if strings.HasSuffix(name, candidate) {
			return strings.TrimSuffix(name, candidate)
		}
	}
	return name
}
