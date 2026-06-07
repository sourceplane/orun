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

// ChangedDot is the small accent dot indicating a directly-changed component
// (the Q2 overlay's "changed" class).
func ChangedDot() string {
	return StyleChangedDot.Render("●")
}

// AffectedDot is the hollow dot indicating a component affected transitively
// via a dependency (the Q2 overlay's "affected" class).
func AffectedDot() string {
	return StyleChangedDot.Render("◌")
}
