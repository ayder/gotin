package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayder/gotin/internal/mapper"
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
