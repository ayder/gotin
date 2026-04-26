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

func TestFooter_NoBridges(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a}}
	got := Render(View{PaneWidth: 30, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	if lines[len(lines)-1] != padLine("dig: u/d/in/out", 30) {
		t.Errorf("footer mismatch: %q", lines[len(lines)-1])
	}
}

func TestFooter_ExistingBridgesAndPartialDigHints(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 0, 1); b.Name = "Stairs"
	a.Exits[mapper.Up] = "b"
	b.Exits[mapper.Down] = "a"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a, "b": b}}
	got := Render(View{PaneWidth: 40, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	want := padLine("↑ Stairs | dig: d/in/out", 40)
	if lines[len(lines)-1] != want {
		t.Errorf("footer mismatch:\nGOT:  %q\nWANT: %q", lines[len(lines)-1], want)
	}
}

func TestFooter_AllFourBridgesNoDigHint(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	a.Exits = map[mapper.Direction]string{
		mapper.Up: "u", mapper.Down: "d", mapper.In: "i", mapper.Out: "o",
	}
	rooms := map[string]*mapper.Room{
		"a": a,
		"u": {ID: "u", Name: "UpRoom", Exits: map[mapper.Direction]string{mapper.Down: "a"}},
		"d": {ID: "d", Name: "DnRoom", Exits: map[mapper.Direction]string{mapper.Up: "a"}},
		"i": {ID: "i", Name: "InRoom", Exits: map[mapper.Direction]string{mapper.Out: "a"}},
		"o": {ID: "o", Name: "OutRoom", Exits: map[mapper.Direction]string{mapper.In: "a"}},
	}
	m := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	got := Render(View{PaneWidth: 60, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	want := padLine("↑ UpRoom | ↓ DnRoom | ▶ InRoom | ◀ OutRoom", 60)
	if lines[len(lines)-1] != want {
		t.Errorf("footer mismatch:\nGOT:  %q\nWANT: %q", lines[len(lines)-1], want)
	}
}

func TestRender_BridgeOverlay_CardinalWinsOverArrow(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 1, 0); b.Name = "B"
	c := makeRoom("c", 0, 0, 1); c.Name = "C"
	a.Exits[mapper.North] = "b"
	b.Exits[mapper.South] = "a"
	a.Exits[mapper.Up] = "c"
	c.Exits[mapper.Down] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b, "c": c},
	}
	got := Render(View{PaneWidth: 18, PaneHeight: 9, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	// Find current row.
	var curRow int
	for i, l := range lines {
		if strings.ContainsRune(l, glyphCurrentRoom) {
			curRow = i
			break
		}
	}
	above := lines[curRow-1]
	if !strings.ContainsRune(above, glyphVLink) {
		t.Errorf("expected │ on row above current room, got %q", above)
	}
	if strings.ContainsRune(above, '↑') {
		t.Errorf("↑ must NOT appear when │ already occupies the gutter, got %q", above)
	}
}

func TestRender_BridgeOverlay_DownAndIn(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 0, 0, -1)
	c := makeRoom("c", 0, 0, 0)
	a.Exits[mapper.Down] = "b"
	b.Exits[mapper.Up] = "a"
	a.Exits[mapper.In] = "c"
	c.Exits[mapper.Out] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b, "c": c},
	}
	got := Render(View{PaneWidth: 18, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, '↓') {
		t.Errorf("expected ↓ overlay below current room:\n%s", got)
	}
	if !strings.ContainsRune(got, '▶') {
		t.Errorf("expected ▶ overlay east of current room:\n%s", got)
	}
}

func TestRender_CoordOverlapNudgesAndFlags(t *testing.T) {
	// a and b both live at (0,0). They share a layer because b lists a as
	// its east neighbour even though the coordinates do not match — that
	// link is omitted by the connectivity rule, but LayerOf still treats
	// them as connected because BFS trusts the exit map.
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 0, 0); b.Name = "B"
	a.Exits[mapper.West] = "b"
	b.Exits[mapper.East] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 22, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, '!') {
		t.Errorf("expected ! overlap flag in render:\n%s", got)
	}
	if strings.Count(got, string(glyphRoom)) < 1 {
		t.Errorf("expected at least one normal room glyph in render:\n%s", got)
	}
}

func TestRender_OmitsEdgeWhenCoordsDoNotMatch(t *testing.T) {
	// A says exits[E]=B, but B's X is 5 (not 1). The link must NOT be drawn.
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 5, 0, 0)
	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 30, PaneHeight: 9, Map: m, CurrentID: "a"})
	if strings.ContainsRune(got, glyphHLink) {
		t.Errorf("expected NO ─ when destination coord does not match offset:\n%s", got)
	}
}

func TestRender_NorthEastDiagonalLink(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 1, 0)
	a.Exits[mapper.NorthEast] = "b"
	b.Exits[mapper.SouthWest] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, glyphDiagNESW) {
		t.Errorf("expected ╱ in render output:\n%s", got)
	}
}

func TestRender_MixedGridAllEightNeighbours(t *testing.T) {
	// 3x3 grid centred on "c" with all eight neighbours.
	rooms := map[string]*mapper.Room{}
	put := func(id string, x, y int) { rooms[id] = makeRoom(id, x, y, 0) }
	put("nw", -1, 1); put("n", 0, 1); put("ne", 1, 1)
	put("w", -1, 0); put("c", 0, 0); put("e", 1, 0)
	put("sw", -1, -1); put("s", 0, -1); put("se", 1, -1)
	c := rooms["c"]
	c.Exits = map[mapper.Direction]string{
		mapper.North: "n", mapper.South: "s", mapper.East: "e", mapper.West: "w",
		mapper.NorthEast: "ne", mapper.NorthWest: "nw",
		mapper.SouthEast: "se", mapper.SouthWest: "sw",
	}
	for id, r := range rooms {
		if id == "c" {
			continue
		}
		// Reverse links so the layer BFS sees them.
		for d, dest := range c.Exits {
			if dest == id {
				r.Exits[mapper.ReverseDirection(d)] = "c"
			}
		}
	}
	m := &mapper.Map{CurrentRoom: "c", Rooms: rooms}
	got := Render(View{PaneWidth: 18, PaneHeight: 11, Map: m, CurrentID: "c"})
	for _, want := range []rune{glyphHLink, glyphVLink, glyphDiagNESW, glyphDiagNWSE, glyphCurrentRoom} {
		if !strings.ContainsRune(got, want) {
			t.Errorf("missing rune %q in render:\n%s", want, got)
		}
	}
	if strings.Count(got, string(glyphCurrentRoom)) != 1 {
		t.Error("expected exactly one current-room glyph")
	}
	if strings.Count(got, string(glyphRoom)) != 8 {
		t.Errorf("expected 8 normal rooms, got %d:\n%s", strings.Count(got, string(glyphRoom)), got)
	}
}

func TestRender_TwoRoomsLinkedEast(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 0, 0)
	a.Name, b.Name = "A", "B"
	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 7, Map: m, CurrentID: "a"})
	if !strings.Contains(got, "▣─■") {
		t.Errorf("expected ▣─■ in body, got:\n%s", got)
	}
}

func TestRender_TwoRoomsLinkedNorth(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 0, 1, 0)
	a.Exits[mapper.North] = "b"
	b.Exits[mapper.South] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 9, Map: m, CurrentID: "a"})
	// Vertical link is in the row above the current room glyph.
	lines := strings.Split(got, "\n")
	bodyStart := 2
	// Find current room row to make assertion robust to centering math.
	var curRow int
	for i := bodyStart; i < len(lines); i++ {
		if strings.ContainsRune(lines[i], glyphCurrentRoom) {
			curRow = i
			break
		}
	}
	if curRow == 0 || curRow-1 < bodyStart {
		t.Fatalf("could not locate current-room row in:\n%s", got)
	}
	if !strings.ContainsRune(lines[curRow-1], glyphVLink) {
		t.Errorf("expected │ on row above current room, got %q", lines[curRow-1])
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
