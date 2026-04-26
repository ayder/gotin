package mappane

import (
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mapper"
)

// padLine right-pads s with spaces so it matches the requested width. Render
// outputs are fixed-size, so test goldens must match exactly.
func padLine(s string, w int) string {
	if n := w - lipglossWidthFallback(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// lipglossWidthFallback measures rune-width for ASCII content. We use it
// because the renderer outputs include Unicode glyphs (■▣╱╲↑) that lipgloss
// counts as 1 cell. Tests can assume single-cell width per rune for the
// glyphs we emit.
func lipglossWidthFallback(s string) int { return len([]rune(s)) }

func joinLines(lines []string) string { return strings.Join(lines, "\n") }

func TestRender_NilMap(t *testing.T) {
	got := Render(View{PaneWidth: 30, PaneHeight: 6})
	want := joinLines([]string{
		padLine("Unnamed Layer", 30),
		padLine("", 30),
		padLine("(no map — try /map create)", 30),
		padLine("", 30),
		padLine("", 30),
		padLine("(no layer bridges)", 30),
	})
	if got != want {
		t.Errorf("nil map render mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRender_MissingCurrent(t *testing.T) {
	m := &mapper.Map{Rooms: map[string]*mapper.Room{}}
	got := Render(View{PaneWidth: 30, PaneHeight: 6, Map: m, CurrentID: "ghost"})
	want := joinLines([]string{
		padLine("Unnamed Layer", 30),
		padLine("", 30),
		padLine("(current room missing from map)", 30),
		padLine("", 30),
		padLine("", 30),
		padLine("(no layer bridges)", 30),
	})
	if got != want {
		t.Errorf("missing current render mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRender_PaneTooShort(t *testing.T) {
	got := Render(View{PaneWidth: 10, PaneHeight: 3})
	want := padLine("pane too short", 10)
	if got != want {
		t.Errorf("pane too short mismatch\nGOT:%q\nWANT:%q", got, want)
	}
}
