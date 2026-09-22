package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/gotin/internal/ui/mappane"
	tea "github.com/charmbracelet/bubbletea"
)

func drawingModel() (Model, *mapper.Map) {
	m := freshModel(100, 30)
	graph := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Name: "Square", Exits: map[mapper.Direction]string{mapper.East: "b"}},
		"b": {ID: "b", Name: "Market", Exits: map[mapper.Direction]string{mapper.West: "a"}},
	}}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return graph, graph.CurrentRoom })
	m.toggleMapPane()
	return m, graph
}

func TestDrawingRunsOutsideUpdateAndCachesAcrossPanResize(t *testing.T) {
	m, _ := drawingModel()
	cmd := m.scheduleMapDrawing(false)
	if cmd == nil || !m.mapDrawPending || m.mapFrame != nil {
		t.Fatal("drawing should be queued, not run during scheduling")
	}
	if !strings.Contains(m.View(), "Drawing map") {
		t.Fatal("missing pending state")
	}
	result := cmd().(mapDrawingMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	updated, _ := m.Update(result)
	m = updated.(Model)
	if m.mapDrawPending || m.mapFrame == nil {
		t.Fatal("completed frame not accepted")
	}
	frame, generation := m.mapFrame, m.mapDrawGeneration
	for _, msg := range []tea.Msg{tea.KeyMsg{Type: tea.KeyShiftRight}, tea.WindowSizeMsg{Width: 120, Height: 40}, StatusMsg{Message: "unrelated output"}} {
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if m.mapFrame != frame || m.mapDrawGeneration != generation || m.mapDrawPending {
			t.Fatal("pan/resize/output restarted solver")
		}
	}
}

func TestMovementCancelsPendingWorkAndRejectsStaleFrames(t *testing.T) {
	m, graph := drawingModel()
	oldCmd := m.scheduleMapDrawing(false)
	oldGeneration := m.mapDrawGeneration
	graph.CurrentRoom = "b"
	newCmd := m.scheduleMapDrawing(false)
	if newCmd == nil || m.mapDrawGeneration == oldGeneration {
		t.Fatal("movement did not replace request")
	}
	oldResult := oldCmd().(mapDrawingMsg)
	if !errors.Is(oldResult.err, context.Canceled) {
		t.Fatalf("old work not cancelled: %v", oldResult.err)
	}
	updated, _ := m.Update(mapDrawingMsg{generation: oldGeneration, frame: &mappane.Frame{Anchor: "stale"}})
	m = updated.(Model)
	if m.mapFrame != nil || !m.mapDrawPending {
		t.Fatal("stale drawing accepted")
	}
	updated, _ = m.Update(newCmd())
	m = updated.(Model)
	if m.mapFrame == nil || m.mapFrame.Anchor != "b" || m.mapDrawPending {
		t.Fatalf("latest frame missing: %v", m.mapDrawError)
	}
}

func TestCloseCancelsDrawingAndRefreshDoesNotMutateEngine(t *testing.T) {
	m, _ := drawingModel()
	cmd := m.scheduleMapDrawing(false)
	updated, _ := m.Update(MapPaneToggleMsg{})
	m = updated.(Model)
	if m.mapDrawPending || m.mapDrawCancel != nil {
		t.Fatal("closing pane left pending work")
	}
	if result := cmd().(mapDrawingMsg); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("worker not cancelled: %v", result.err)
	}

	engine := mapper.NewEngine("")
	if err := engine.Create(""); err != nil {
		t.Fatal(err)
	}
	if err := engine.Dig(mapper.North, "Next"); err != nil {
		t.Fatal(err)
	}
	before, _ := engine.Snapshot()
	m.SetMapEngineSnapshot(engine.Snapshot)
	updated, _ = m.Update(MapPaneToggleMsg{})
	m = updated.(Model)
	defer m.cancelMapDrawing()
	generation := m.mapDrawGeneration
	updated, _ = m.Update(MapPaneRefreshMsg{})
	m = updated.(Model)
	if m.mapDrawGeneration <= generation {
		t.Fatal("refresh did not request a fresh solve")
	}
	after, _ := engine.Snapshot()
	for id, r := range before.Rooms {
		if after.Rooms[id].X != r.X || after.Rooms[id].Y != r.Y {
			t.Fatal("refresh modified discovery coordinates")
		}
	}
	if err := engine.Undo(); err != nil {
		t.Fatal(err)
	}
	after, _ = engine.Snapshot()
	if len(after.Rooms) != 1 {
		t.Fatal("refresh consumed undo; dig should have been undone")
	}
}

func TestDrawingFailureDoesNotBlockDiscoveryOrAutomaticallyRetry(t *testing.T) {
	m, graph := drawingModel()
	graph.Rooms["b"].Exits[mapper.East] = "a" // Contradictory eastward loop.
	cmd := m.scheduleMapDrawing(false)
	result := cmd().(mapDrawingMsg)
	if result.err == nil {
		t.Fatal("expected conflicting topology error")
	}
	updated, _ := m.Update(result)
	m = updated.(Model)
	if m.mapDrawPending || m.mapDrawError == nil || len(graph.Rooms) != 2 || graph.CurrentRoom != "a" {
		t.Fatal("failure modified discovery or retried")
	}
	if m.scheduleMapDrawing(false) != nil {
		t.Fatal("error retried on every redraw")
	}
	graph.Rooms["b"].Exits = map[mapper.Direction]string{mapper.West: "a"}
	if m.scheduleMapDrawing(false) == nil {
		t.Fatal("corrected topology did not trigger redraw")
	}
	m.cancelMapDrawing()
}
