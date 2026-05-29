package theme

// StatusGlyph returns a colored unicode glyph for the canonical lifecycle
// states surfaced across the cockpit (Browse, Run Dashboard, History).
//
//	"running"             → ◐ cyan
//	"success"/"completed" → ● green
//	"failed"/"error"      → ● red
//	"" / anything else    → ○ dim
func StatusGlyph(status string) string {
	switch status {
	case "running", "in_progress":
		return StyleStatusRunning.Render("◐")
	case "success", "completed", "ok":
		return StyleStatusOK.Render("●")
	case "failed", "error":
		return StyleStatusFail.Render("●")
	default:
		return StyleStatusIdle.Render("○")
	}
}

// ChangedDot is the small accent dot indicating ComponentSummary.Changed.
func ChangedDot() string {
	return StyleChangedDot.Render("●")
}
