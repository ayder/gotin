package mappane

import (
	"context"
	"github.com/charmbracelet/x/ansi"
	"nelib"
	"reflect"
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

func drawnView(t *testing.T, m *mapper.Map, current, remote string) View {
	t.Helper()
	a := &Adapter{}
	req, err := a.Prepare(m, current, remote)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Draw(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return View{Map: m, CurrentID: current, LayerKey: remote, Frame: frame, PaneWidth: 80, PaneHeight: 24}
}

func TestNelibDrawingIgnoresDiscoveryCoordinates(t *testing.T) {
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Name: "Square", X: 7, Y: 11, Exits: map[mapper.Direction]string{mapper.South: "b", mapper.West: "c"}},
		"b": {ID: "b", X: 7, Y: 11, Exits: map[mapper.Direction]string{mapper.North: "a", mapper.NorthWest: "c"}},
		"c": {ID: "c", X: 7, Y: 11, Exits: map[mapper.Direction]string{mapper.SouthEast: "b", mapper.East: "a"}},
	}}
	v := drawnView(t, m, "a", "")
	if _, _, err := nelib.ParseDrawing(v.Frame.Drawing.Text); err != nil {
		t.Fatal(err)
	}
	for id, r := range m.Rooms {
		if r.X != 7 || r.Y != 11 {
			t.Fatalf("changed discovery coordinates for %s", id)
		}
	}
	out := Render(v)
	if !strings.Contains(out, "●") || !strings.Contains(out, "╲") || !strings.Contains(out, "│") || !strings.Contains(out, "─") {
		t.Fatalf("missing map geometry:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) != v.PaneWidth {
			t.Fatalf("wrong width: %q", line)
		}
	}
	v.PanOffset = Point{Col: 1000}
	out = Render(v)
	if strings.Contains(out, "●") || !strings.Contains(out, "*") {
		t.Fatalf("pan/clipped marker:\n%s", out)
	}
}

func TestRemoteLayerAndBridgeFooter(t *testing.T) {
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Name: "Square", Exits: map[mapper.Direction]string{mapper.Up: "b", mapper.In: "c"}},
		"b": {ID: "b", Name: "Loft", Z: 1, Exits: map[mapper.Direction]string{}},
		"c": {ID: "c", Name: "Shop", Exits: map[mapper.Direction]string{mapper.Out: "a"}},
	}}
	current := drawnView(t, m, "a", "")
	out := Render(current)
	if !strings.Contains(current.Frame.Drawing.Text, "↑") || !strings.Contains(out, "↑ Loft") || !strings.Contains(out, "▶ Shop") {
		t.Fatalf("missing bridges:\n%s", out)
	}
	remote := drawnView(t, m, "a", "b")
	out = Render(remote)
	if !strings.Contains(out, "Loft [Z=1] (viewing remote)") || strings.Contains(out, "●") {
		t.Fatalf("remote marker/header:\n%s", out)
	}
	if len(remote.Frame.Drawing.Positions) != 1 {
		t.Fatal("other layer leaked into view")
	}
}

func TestOneWayLinkVisibleFromDestination(t *testing.T) {
	m := &mapper.Map{CurrentRoom: "b", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Exits: map[mapper.Direction]string{mapper.East: "b"}},
		"b": {ID: "b", Exits: map[mapper.Direction]string{}},
	}}
	v := drawnView(t, m, "b", "")
	if !strings.Contains(v.Frame.Drawing.Text, "→") || len(v.Frame.Drawing.Positions) != 2 {
		t.Fatalf("one-way passage missing: %s", v.Frame.Drawing.Text)
	}
	if len(m.Rooms["b"].Exits) != 0 {
		t.Fatal("adapter invented reverse exit")
	}
	if len(AllLayers(m)) != 1 {
		t.Fatal("one-way connection split into separate layers")
	}
}

func TestAdapterStableLabelsAndCacheKey(t *testing.T) {
	m := &mapper.Map{CurrentRoom: "z", Rooms: map[string]*mapper.Room{"z": {ID: "z", Name: "First", Exits: map[mapper.Direction]string{}}}}
	a := &Adapter{}
	first, err := a.Prepare(m, "z", "")
	if err != nil {
		t.Fatal(err)
	}
	m.Rooms["z"].Name = "Renamed"
	m.Rooms["z"].X = 500
	same, _ := a.Prepare(m, "z", "")
	if first.Key != same.Key {
		t.Fatal("metadata/recognition coordinates invalidated drawing")
	}
	m.Rooms["a"] = &mapper.Room{ID: "a", Exits: map[mapper.Direction]string{}}
	added, _ := a.Prepare(m, "z", "")
	if first.Scene.Rooms["z"].Label != added.Scene.Rooms["z"].Label {
		t.Fatal("existing room renumbered")
	}
	if reflect.DeepEqual(added.Key, first.Key) {
		t.Fatal("graph change did not invalidate drawing")
	}
	m.Rooms["z"].Exits[mapper.North] = "a"
	if len(first.Scene.Rooms["z"].Exits) != 0 {
		t.Fatal("request aliases caller maps")
	}
}

func TestFailureAndPendingDoNotRenderOldGeometry(t *testing.T) {
	m := &mapper.Map{Rooms: map[string]*mapper.Room{"a": {ID: "a", Exits: map[mapper.Direction]string{}}}}
	v := drawnView(t, m, "a", "")
	v.Error = nelib.ErrImpossible
	out := Render(v)
	if !strings.Contains(out, "conflicting exits") || !strings.Contains(out, "Discovered rooms are retained") || strings.Contains(out, "●") {
		t.Fatalf("error state: %s", out)
	}
	v.Error = nil
	v.Pending = true
	out = Render(v)
	if !strings.Contains(out, "Drawing map") || strings.Contains(out, "●") {
		t.Fatal("pending state displayed stale geometry")
	}
}

func TestHeaderUsesTerminalCellWidth(t *testing.T) {
	if got := padRight("界界界", 5); ansi.StringWidth(got) != 5 {
		t.Fatalf("wrong width: %q", got)
	}
}
