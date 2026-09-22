package ui

import (
	"context"
	"time"

	"github.com/ayder/gotin/internal/ui/mappane"
	tea "github.com/charmbracelet/bubbletea"
)

type mapDrawingMsg struct {
	generation uint64
	frame      *mappane.Frame
	err        error
}

// Update schedules drawing work only after applying input/state messages.
// Generation checks discard results from cancelled or superseded requests.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	force := false
	switch result := msg.(type) {
	case mapDrawingMsg:
		if result.generation == m.mapDrawGeneration {
			m.mapFrame, m.mapDrawError = result.frame, result.err
			m.mapDrawPending = false
			if m.mapDrawCancel != nil {
				m.mapDrawCancel()
				m.mapDrawCancel = nil
			}
		}
		cmd := m.scheduleMapDrawing(false)
		return m, cmd
	case MapPaneRefreshMsg:
		force = true
	}
	updated, cmd := m.update(msg)
	next := updated.(Model)
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		next.cancelMapDrawing()
		return next, cmd
	}
	draw := next.scheduleMapDrawing(force)
	return next, tea.Batch(cmd, draw)
}

func (m *Model) cancelMapDrawing() {
	if m.mapDrawCancel != nil {
		m.mapDrawCancel()
		m.mapDrawCancel = nil
	}
	if m.mapDrawPending {
		m.mapDrawGeneration++
		m.mapDrawHasKey = false
	}
	m.mapDrawPending = false
}

func (m *Model) scheduleMapDrawing(force bool) tea.Cmd {
	if force {
		m.mapDrawHasKey = false
	}
	if !m.mapPaneVisible || m.mapEngineSnapshot == nil {
		m.cancelMapDrawing()
		return nil
	}
	snap, current := m.mapEngineSnapshot()
	m.mapSnapshot, m.mapCurrent = snap, current
	if current != m.mapPaneLastCurrID {
		m.mapPanOffset = mappane.Point{}
		m.mapPaneLayerKey = ""
		m.mapPaneLastCurrID = current
	}
	if m.mapAdapter == nil {
		m.mapAdapter = &mappane.Adapter{}
	}
	req, err := m.mapAdapter.Prepare(snap, current, m.mapPaneLayerKey)
	if err != nil {
		m.cancelMapDrawing()
		m.mapDrawHasKey = false
		m.mapFrame, m.mapDrawError = nil, err
		return nil
	}
	if m.mapDrawHasKey && req.Key == m.mapDrawKey {
		return nil
	}
	m.cancelMapDrawing()
	m.mapDrawGeneration++
	m.mapDrawKey, m.mapDrawHasKey = req.Key, true
	m.mapFrame, m.mapDrawError, m.mapDrawPending = nil, nil, true
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	m.mapDrawCancel = cancel
	generation := m.mapDrawGeneration
	return func() tea.Msg {
		defer cancel()
		frame, err := mappane.Draw(ctx, req)
		return mapDrawingMsg{generation: generation, frame: frame, err: err}
	}
}
