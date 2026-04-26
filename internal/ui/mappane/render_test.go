package mappane

import (
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mapper"
)

// padLine right-pads or truncates s so it matches the requested width. Render
// outputs are fixed-size, so test goldens must match exactly.
func padLine(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	if n := w - len(r); n > 0 {
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

func TestRender_SingleRoomCentered(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	a.Name = "Town Square"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a},
	}
	w, h := 12, 7
	got := Render(View{PaneWidth: w, PaneHeight: h, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	if len(lines) != h {
		t.Fatalf("expected %d lines, got %d", h, len(lines))
	}
	if lines[0] != padLine("Town Square", w) {
		t.Errorf("header[0] = %q", lines[0])
	}
	if lines[1] != padLine("Town Square", w) {
		t.Errorf("header[1] = %q", lines[1])
	}
	// Body rows are h - 3 = 4. Center body row = (4-1)/2 snapped to even = 2.
	bodyTop := 2
	wantCenterRow := bodyTop + 2 // bodyRows/2 = 2 → header offset 2 → row 4
	if !strings.Contains(lines[wantCenterRow], "▣") {
		t.Errorf("expected current-room glyph ▣ on line %d, got %q", wantCenterRow, lines[wantCenterRow])
	}
}
