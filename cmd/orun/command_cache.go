package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sourceplane/orun/internal/cachehome"
	"github.com/spf13/cobra"
)

var (
	cacheLsKind     string
	cacheLsJSON     bool
	cachePruneAge   string
	cachePruneKind  string
	cachePruneDry   bool
	cacheGCMaxSize  string
	cacheGCDry      bool
	cacheDefaultGC  = "10G"
	cacheDefaultAge = "30d"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "List, prune and bound the caches orun keeps",
	Long: `The caches orun keeps on this machine — remote actions, the tool cache,
the shared stores, declared caches, composition packages — and in this
checkout (.orun/runs, the resolve memo). ls lists them with their size and
when they were last used; prune removes those not used for a while; gc keeps
the machine-wide caches under a size budget, least recently used first.`,
}

var cacheLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List every cache entry with its size and last use",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := cacheInventory()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		entries = filterCacheKind(entries, cacheLsKind)
		if cacheLsJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(entries)
		}
		printCacheTable(entries, true)
		return nil
	},
}

var cachePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove cache entries not used for a while (default 30d)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		age, err := parseAge(cachePruneAge)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
		entries, err := cacheInventory()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		selected := cachehome.OlderThan(filterCacheKind(entries, cachePruneKind), age, time.Now())
		return removeCacheEntries(selected, cachePruneDry, fmt.Sprintf("not used for %s", cachePruneAge))
	},
}

var cacheGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Keep the machine-wide caches under a size budget (default 10G), least recently used first",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		budget, err := parseSize(cacheGCMaxSize)
		if err != nil {
			return fmt.Errorf("--max-size: %w", err)
		}
		entries, err := cacheInventory()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		var machine []cachehome.Entry
		for _, e := range entries {
			if e.Kind.MachineWide() {
				machine = append(machine, e)
			}
		}
		selected := cachehome.OverBudget(machine, budget)
		fmt.Printf("%s under %s: %s\n", cachehome.Root(), formatSize(cachehome.Total(machine)), budgetVerdict(cachehome.Total(machine), budget))
		return removeCacheEntries(selected, cacheGCDry, "over the budget")
	},
}

func registerCacheCommand(root *cobra.Command) {
	root.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheLsCmd, cachePruneCmd, cacheGCCmd)
	cacheLsCmd.Flags().StringVar(&cacheLsKind, "kind", "", "Only this kind: "+kindList())
	cacheLsCmd.Flags().BoolVar(&cacheLsJSON, "json", false, "Print the entries as JSON")
	cachePruneCmd.Flags().StringVar(&cachePruneAge, "older-than", cacheDefaultAge, "Remove entries last used before this long ago (30d, 12h, 90m)")
	cachePruneCmd.Flags().StringVar(&cachePruneKind, "kind", "", "Only this kind: "+kindList())
	cachePruneCmd.Flags().BoolVar(&cachePruneDry, "dry-run", false, "Print what would be removed without removing anything")
	cacheGCCmd.Flags().StringVar(&cacheGCMaxSize, "max-size", cacheDefaultGC, "The budget for everything under ~/.orun (10G, 500M)")
	cacheGCCmd.Flags().BoolVar(&cacheGCDry, "dry-run", false, "Print what would be removed without removing anything")
}

// cacheInventory lists the machine's caches and, when the working directory
// (or --intent's directory) holds a .orun, that checkout's.
func cacheInventory() ([]cachehome.Entry, error) {
	checkout, err := filepath.Abs(storeDir())
	if err != nil {
		checkout = ""
	}
	if checkout != "" {
		if _, err := os.Stat(filepath.Join(checkout, ".orun")); err != nil {
			checkout = ""
		}
	}
	return cachehome.Inventory(checkout)
}

func filterCacheKind(entries []cachehome.Entry, kind string) []cachehome.Entry {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return entries
	}
	var out []cachehome.Entry
	for _, e := range entries {
		if string(e.Kind) == kind {
			out = append(out, e)
		}
	}
	return out
}

func removeCacheEntries(selected []cachehome.Entry, dryRun bool, why string) error {
	if len(selected) == 0 {
		fmt.Printf("Nothing to remove (%s).\n", why)
		return nil
	}
	verb := "Removing"
	if dryRun {
		verb = "Would remove"
	}
	fmt.Printf("%s %d entries, %s (%s):\n", verb, len(selected), formatSize(cachehome.Total(selected)), why)
	printCacheTable(selected, false)
	if dryRun {
		return nil
	}
	var failed int
	var removed int64
	for _, e := range selected {
		if err := cachehome.Remove(e); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  skipped %s %s: %v\n", e.Kind, e.Name, err)
			continue
		}
		removed += e.Size
	}
	fmt.Printf("Removed %s", formatSize(removed))
	if failed > 0 {
		fmt.Printf(" (%d skipped)", failed)
	}
	fmt.Println()
	return nil
}

func printCacheTable(entries []cachehome.Entry, totals bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tENTRY\tSIZE\tLAST USED\t")
	for _, e := range entries {
		note := ""
		if e.InUse {
			note = "in use"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Kind, e.Name, formatSize(e.Size), formatAgo(e.LastUsed), note)
	}
	_ = w.Flush()
	if !totals {
		return
	}
	byKind := map[cachehome.Kind]int64{}
	counts := map[cachehome.Kind]int{}
	for _, e := range entries {
		byKind[e.Kind] += e.Size
		counts[e.Kind]++
	}
	var kinds []cachehome.Kind
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	fmt.Println()
	for _, k := range kinds {
		fmt.Printf("%-14s %4d entries  %s\n", k, counts[k], formatSize(byKind[k]))
	}
	fmt.Printf("%-14s %4d entries  %s\n", "total", len(entries), formatSize(cachehome.Total(entries)))
}

func kindList() string {
	parts := make([]string, 0, len(cachehome.Kinds))
	for _, k := range cachehome.Kinds {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ", ")
}

func budgetVerdict(total, budget int64) string {
	if total <= budget {
		return "within the budget of " + formatSize(budget)
	}
	return "over the budget of " + formatSize(budget) + " by " + formatSize(total-budget)
}

// parseAge reads 30d, 12h, 90m or any Go duration.
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a duration (30d, 12h, 90m)", s)
	}
	return d, nil
}

// parseSize reads 10G, 500M, 1.5G, 64K or a number of bytes.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimSuffix(s, "IB")
	s = strings.TrimSuffix(s, "B")
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "T"):
		mult, s = 1<<40, strings.TrimSuffix(s, "T")
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a size (10G, 500M)", s)
	}
	return int64(n * float64(mult)), nil
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func formatAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
