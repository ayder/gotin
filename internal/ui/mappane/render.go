package mappane

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// Render returns a styled string sized exactly View.PaneWidth × View.PaneHeight.
// Pure: same input → same output. No goroutines, no I/O.
func Render(v View) string {
	if v.PaneHeight < 5 {
		return padRight("pane too short", v.PaneWidth)
	}

	header := layerHeaderLines(v)
	footer := footerLine(v)
	bodyRows := v.PaneHeight - len(header) - 1 // 1 row for footer
	body := bodyLines(v, bodyRows)

	all := make([]string, 0, v.PaneHeight)
	all = append(all, header...)
	all = append(all, body...)
	all = append(all, footer)
	return strings.Join(all, "\n")
}

// padRight right-pads s with spaces to width w. Truncates if longer.
func padRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
