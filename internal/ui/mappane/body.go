package mappane

import (
	"context"
	"errors"
	"strings"

	"github.com/ayder/nelib"
)

// bodyLines only crops a completed drawing. It never solves, starts a worker,
// or touches the discovery engine.
func bodyLines(v View, rows int) []string {
	if rows <= 0 {
		return nil
	}
	blank := make([]string, rows)
	for i := range blank {
		blank[i] = padRight("", v.PaneWidth)
	}
	message := ""
	switch {
	case v.Map == nil:
		message = "(no map — try /map create)"
	case v.Map.Rooms[v.CurrentID] == nil:
		message = "(current room missing from map)"
	case v.Error != nil:
		switch {
		case errors.Is(v.Error, nelib.ErrImpossible), errors.Is(v.Error, nelib.ErrDirectionConflict), errors.Is(v.Error, nelib.ErrLayerConflict):
			message = "Map has conflicting exits."
		case errors.Is(v.Error, nelib.ErrSearchLimit), errors.Is(v.Error, context.DeadlineExceeded):
			message = "Layout timed out. Ctrl-r retries."
		case errors.Is(v.Error, nelib.ErrRenderLimit):
			message = "Map exceeds drawing limit."
		default:
			message = "Cannot draw map: " + v.Error.Error()
		}
		if rows > 1 {
			blank[1] = padRight("Discovered rooms are retained.", v.PaneWidth)
		}
	case v.Pending || v.Frame == nil:
		message = "Drawing map…"
	}
	if message != "" {
		blank[0] = padRight(message, v.PaneWidth)
		return blank
	}
	drawing := v.Frame.Drawing
	anchor, ok := drawing.Positions[v.Frame.Anchor]
	if !ok {
		return blank
	}
	origin := nelib.Point{X: anchor.X - int64(v.PaneWidth/2) + int64(v.PanOffset.Col), Y: anchor.Y - int64(rows/2) + int64(v.PanOffset.Row)}
	text, err := drawing.Viewport(origin, v.PaneWidth, rows)
	if err != nil {
		blank[0] = padRight("Pane cannot be drawn at this size.", v.PaneWidth)
		return blank
	}
	lines := strings.Split(text, "\n")
	if current, ok := drawing.Positions[v.CurrentID]; ok {
		x, y := current.X-origin.X, current.Y-origin.Y
		if x < 0 || y < 0 || x >= int64(v.PaneWidth) || y >= int64(rows) {
			// Gotin scenes use single-cell numeric labels and no lock glyphs.
			col, row := max(0, min(v.PaneWidth-1, int(x))), max(0, min(rows-1, int(y)))
			runes := []rune(lines[row])
			runes[col] = '*'
			lines[row] = string(runes)
		}
	}
	return lines
}
