// Package mappane is a pure renderer that turns a *mapper.Map snapshot into
// a styled, fixed-size string suitable for placement in a Bubble Tea TUI.
// The package never holds the mapper engine lock and never mutates the map.
package mappane

import "github.com/ayder/gotin/internal/mapper"

// Point is a (col, row) pair in screen-cell units, not coordinate units.
type Point struct{ Col, Row int }

// View is the complete input contract for Render. Same View → same output.
type View struct {
	PaneWidth  int         // total cols including borders/padding
	PaneHeight int         // total rows
	Map        *mapper.Map // map snapshot; may be nil
	CurrentID  string      // current room ID; "" or missing → fallback render
	PanOffset  Point       // pan in screen cells from current-room-centred default
	Frame      *Frame      // Completed nelib drawing; never computed by Render.
	Pending    bool
	Error      error
	LayerKey   string // representative room ID of layer to render; "" = layer of CurrentID
}
