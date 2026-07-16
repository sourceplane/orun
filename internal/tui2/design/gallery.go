package design

import (
	"strings"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Gallery renders every Northwind Mono component with fixture data at
// width — the terminal counterpart of the console's /demo page. The golden
// tests pin it at 80/120/220 columns in light and dark, so any visual
// change to the kit is a reviewed diff, never an accident.
func Gallery(width int) string {
	var s []string
	section := func(title string) {
		s = append(s, "", " "+Title.Render(title), "")
	}

	section("tones")
	tones := []Tone{ToneNeutral, ToneInfo, ToneSuccess, ToneWarning, ToneError, ToneLive}
	var chips []string
	for _, t := range tones {
		chips = append(chips, Pill(t, t.String()))
	}
	s = append(s, "  "+strings.Join(chips, Muted.Render(" · ")))

	section("status")
	var st []string
	for _, v := range []string{"completed", "failed", "running", "pending", "skipped"} {
		st = append(st, StatusText(v))
	}
	s = append(s, "  "+strings.Join(st, "   "))

	section("data rows")
	s = append(s,
		DataRow(width, true, "checkout-service", "payments · deploy", StatusText("running")),
		DataRow(width, false, "catalog-sync", "platform · plan", StatusText("completed")),
		DataRow(width, false, "web-frontend", "storefront · deploy", StatusText("failed")),
	)

	section("table")
	s = append(s, Table(width,
		[]Column{{Title: "Run", Width: 0}, {Title: "Env", Width: 12}, {Title: "Status", Width: 14}},
		[][]string{
			{"deploy checkout " + Ref.Render("01J8Z3"), "production", StatusText("running")},
			{"plan payments " + Ref.Render("01J8Z2"), "staging", StatusText("completed")},
			{"deploy web " + Ref.Render("01J8Z1"), "production", StatusText("failed")},
		}, 0))

	section("stat tiles")
	tileW := (width - 4) / 3
	tiles := []string{
		StatTile(tileW, "Components", "24", "3 changed"),
		StatTile(tileW, "Live sessions", "2", "1 needs you"),
		StatTile(tileW, "Last run", "✓ 12m ago", "deploy checkout"),
	}
	rows := make([]string, 3)
	for _, tile := range tiles {
		for i, l := range strings.Split(tile, "\n") {
			rows[i] += " " + l
		}
	}
	s = append(s, rows...)

	section("filter chips")
	s = append(s, "  "+Chips(1, "all", "running", "failed", "needs you"))

	section("entity kinds")
	var kinds []string
	for _, k := range []string{"Component", "API", "System", "Environment"} {
		kinds = append(kinds, Kind(k))
	}
	s = append(s, "  "+strings.Join(kinds, "   "))

	section("markdown")
	s = append(s, Markdown("## Rollout plan\nDeploy `checkout` then verify:\n- canary at 5%\n- full fleet\n> sealed 2026-07-14", width-4))

	section("dialog")
	s = append(s, Dialog("Run plan", []string{"4 jobs across 2 environments.", Dim.Render("changed components only")}, "Run 4 jobs", frame.Size{Width: width, Height: 12}))

	section("key hints")
	s = append(s, "  "+KeyHint("r", "run")+"   "+KeyHint("g", "compose")+"   "+KeyHint("i", "inspect"))

	return strings.Join(s, "\n")
}
