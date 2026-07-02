package main

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// secretKeyPattern is the shipped KEY shape (specs/orun-secrets/data-model.md
// §1): a letter, then up to 127 of [A-Za-z0-9._-].
var secretKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

// dotenvEntry is one parsed KEY=VALUE line of a dotenv file. Invalid entries
// carry a Reason that NEVER includes the value — dotenv files hold secrets and
// nothing from the right-hand side may reach output or errors.
type dotenvEntry struct {
	Key     string
	Value   string
	Line    int
	Invalid bool
	Reason  string
}

// parseDotenv parses dotenv content: blank lines and # comments are skipped,
// an optional `export ` prefix is stripped, CRLF line endings are tolerated,
// and a single pair of surrounding quotes ("…" or '…') is stripped from the
// value. Keys not matching the secret KEY shape (and lines without `=`)
// become per-key invalid entries rather than failing the batch.
func parseDotenv(r io.Reader) ([]dotenvEntry, error) {
	var entries []dotenvEntry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))

		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			entries = append(entries, dotenvEntry{
				// Only the left-hand side of a would-be assignment is ever
				// echoed; a line without '=' has no value part by definition.
				Key:     trimmed,
				Line:    lineNo,
				Invalid: true,
				Reason:  "not KEY=VALUE",
			})
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		value := stripSurroundingQuotes(strings.TrimSpace(trimmed[eq+1:]))

		if !secretKeyPattern.MatchString(key) {
			entries = append(entries, dotenvEntry{
				Key:     key,
				Line:    lineNo,
				Invalid: true,
				Reason:  "invalid key (must match ^[A-Za-z][A-Za-z0-9._-]{0,127}$)",
			})
			continue
		}
		entries = append(entries, dotenvEntry{Key: key, Value: value, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading dotenv: %w", err)
	}
	return entries, nil
}

// stripSurroundingQuotes removes exactly one matching pair of surrounding
// double or single quotes. Mismatched or unbalanced quotes are left as-is.
func stripSurroundingQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
