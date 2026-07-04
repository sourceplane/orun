package main

// catalog_help_test.go fixture-tests the --help output of every `orun catalog`
// subcommand (cli-surface.md §12) so help text — one-line summary, longer
// description, flag list, examples, and the Exit codes section — cannot drift
// silently. Golden files live under testdata/help/catalog/<name>.txt.
//
// Regenerate after an intentional help change with:
//
//	go test ./cmd/orun/ -run TestCatalogHelpFixtures -update
//
// The harness renders each command's help into a buffer via cobra's own
// help machinery (the same path `orun catalog <sub> --help` takes), so the
// fixtures capture exactly what a user sees.

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

var updateHelpFixtures = flag.Bool("update", false, "regenerate catalog help golden files")

// catalogSubcommandNames is the closed set of subcommands the §12 index must
// expose. A drift here (added/removed subcommand) fails the index test below.
var catalogSubcommandNames = []string{
	"refresh", "push", "list", "describe", "docs", "tree", "history", "validate", "diff", "refs", "affected", "migrate",
}

func findCatalogCommand(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "catalog" {
			return c
		}
	}
	t.Fatal("catalog command not registered on root")
	return nil
}

func renderHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("render help for %q: %v", cmd.Name(), err)
	}
	return buf.String()
}

func TestCatalogHelpFixtures(t *testing.T) {
	catalogCmd := findCatalogCommand(t)

	// The root `orun catalog` help (subcommand index) plus each subcommand.
	cmds := map[string]*cobra.Command{"catalog": catalogCmd}
	for _, sub := range catalogCmd.Commands() {
		cmds[sub.Name()] = sub
	}

	dir := filepath.Join("testdata", "help", "catalog")
	if *updateHelpFixtures {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for name, cmd := range cmds {
		name, cmd := name, cmd
		t.Run(name, func(t *testing.T) {
			got := renderHelp(t, cmd)
			path := filepath.Join(dir, name+".txt")

			if *updateHelpFixtures {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write fixture %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v (run with -update to generate)", path, err)
			}
			if got != string(want) {
				t.Errorf("help for %q drifted from %s.\n--- got ---\n%s\n--- want ---\n%s",
					name, path, got, string(want))
			}
		})
	}
}

// TestCatalogSubcommandIndex asserts the §12 subcommand set is exactly the
// expected closed list — catches an unregistered or stray subcommand even if
// its individual fixture happens to exist.
func TestCatalogSubcommandIndex(t *testing.T) {
	catalogCmd := findCatalogCommand(t)
	got := map[string]bool{}
	for _, sub := range catalogCmd.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range catalogSubcommandNames {
		if !got[want] {
			t.Errorf("missing catalog subcommand %q", want)
		}
		delete(got, want)
	}
	for extra := range got {
		t.Errorf("unexpected catalog subcommand %q (update catalogSubcommandNames if intentional)", extra)
	}
}
