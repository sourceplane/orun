package ui

import (
	"io"
	"os"
	"strings"
)

func ColorEnabledForWriter(w io.Writer) bool {
	if force := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")); force != "" && force != "0" {
		return true
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("ORUN_NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CLICOLOR")), "0") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}

	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func Style(enabled bool, text string, codes ...string) string {
	if !enabled || text == "" || len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

func Bold(enabled bool, text string) string {
	return Style(enabled, text, "1")
}

func Dim(enabled bool, text string) string {
	return Style(enabled, text, "2")
}

func Red(enabled bool, text string) string {
	return Style(enabled, text, "31")
}

func Green(enabled bool, text string) string {
	return Style(enabled, text, "32")
}

func Yellow(enabled bool, text string) string {
	return Style(enabled, text, "33")
}

func Blue(enabled bool, text string) string {
	return Style(enabled, text, "34")
}

func Magenta(enabled bool, text string) string {
	return Style(enabled, text, "35")
}

func Cyan(enabled bool, text string) string {
	return Style(enabled, text, "36")
}

func BoldCyan(enabled bool, text string) string {
	return Style(enabled, text, "1", "36")
}

func IsInteractiveWriter(w io.Writer) bool {
	if isCI() {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func isCI() bool {
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "BUILDKITE", "JENKINS_URL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" && !strings.EqualFold(v, "false") {
			return true
		}
	}
	return false
}
