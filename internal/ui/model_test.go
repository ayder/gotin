package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/gotin/internal/ui/mappane"
)

func freshModel(termW, termH int) Model {
	m := New(nil, nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: termW, Height: termH})
	return tm.(Model)
}

func TestMapPaneToggle_VisibleAt80x24(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if !m.mapPaneVisible {
		t.Fatal("pane should be visible after toggle")
	}
	if m.mapPaneWidth == 0 {
		t.Fatal("expected nonzero pane width")
	}
	if m.viewport.Width != 80-m.mapPaneWidth-1 {
		t.Errorf("chat viewport width = %d, want %d", m.viewport.Width, 80-m.mapPaneWidth-1)
	}
}

func TestMapPaneToggle_RejectsNarrowTerminal(t *testing.T) {
	m := freshModel(50, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Error("pane must not toggle on at width<65")
	}
}

func TestMapPaneToggle_OffRestoresFullViewport(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Fatal("expected pane invisible after second toggle")
	}
	if m.viewport.Width != 80 {
		t.Errorf("expected viewport width restored to 80, got %d", m.viewport.Width)
	}
}

// quiet unused-import: mapper used by later tests.
var _ = mapper.Direction("")

func TestStatusMsg_TriggersRecenterWhenCurrentRoomChanges(t *testing.T) {
	m := freshModel(80, 24)
	curr := "a"
	rooms := map[string]*mapper.Room{
		"a": {ID: "a", Name: "A", Exits: map[mapper.Direction]string{}},
		"b": {ID: "b", Name: "B", Exits: map[mapper.Direction]string{}},
	}
	mm := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, curr })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.mapPanOffset = mappane.Point{Col: 6, Row: 6}
	m.mapPaneLastCurrID = "a"

	// Simulate a movement; current ID flips.
	curr = "b"
	mm.CurrentRoom = "b"
	tm, _ = m.Update(StatusMsg{Message: "[Map] Moved to: B"})
	m = tm.(Model)

	if m.mapPanOffset != (mappane.Point{}) {
		t.Errorf("expected recenter on room change, offset = %+v", m.mapPanOffset)
	}
	if m.mapPaneLastCurrID != "b" {
		t.Errorf("expected lastCurrID=b, got %q", m.mapPaneLastCurrID)
	}
}

func TestPaneKeys_EscClosesWhenEmpty(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Error("Esc did not close pane")
	}
}

func TestPaneKeys_EscDoesNotCloseWhenTyping(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.textinput.SetValue("look")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if !m.mapPaneVisible {
		t.Error("Esc must not close pane while user is typing")
	}
}

func TestPaneKeys_PanOnlyWhenInputEmpty(t *testing.T) {
	m := freshModel(80, 24)
	mm := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Exits: map[mapper.Direction]string{}},
	}}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)

	// Pane visible, input empty: 'l' should pan +2 cols.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = tm.(Model)
	if m.mapPanOffset.Col != 2 {
		t.Errorf("pan col = %d, want 2", m.mapPanOffset.Col)
	}
	if m.textinput.Value() != "" {
		t.Errorf("textinput must remain empty, got %q", m.textinput.Value())
	}

	// Now type some characters → pane keys must NOT pan.
	m.textinput.SetValue("look")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = tm.(Model)
	if m.mapPanOffset.Col != 2 {
		t.Errorf("pan col changed while typing: got %d", m.mapPanOffset.Col)
	}
}

func TestPaneKeys_RecenterOnC(t *testing.T) {
	m := freshModel(80, 24)
	mm := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Exits: map[mapper.Direction]string{}},
	}}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.mapPanOffset = mappane.Point{Col: 4, Row: 4}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = tm.(Model)
	if m.mapPanOffset != (mappane.Point{}) {
		t.Errorf("expected pan reset on c, got %+v", m.mapPanOffset)
	}
}

func TestMapPaneView_RendersSplit(t *testing.T) {
	m := freshModel(80, 24)
	// Inject a tiny snapshot accessor so the pane has something to draw.
	rooms := map[string]*mapper.Room{
		"a": {ID: "a", Name: "Square", Exits: map[mapper.Direction]string{}},
	}
	mm := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	out := m.View()
	if !strings.Contains(out, "Square") {
		t.Errorf("expected pane header to contain Square in:\n%s", out)
	}
}
