package mappane

import (
	"sort"

	"github.com/ayder/gotin/internal/mapper"
)

// layerHeaderLines returns the two header rows: layer label + current room
// name. Both are padded to PaneWidth.
func layerHeaderLines(v View) []string {
	label := "Unnamed Layer"
	currentName := ""
	if v.Map != nil && v.CurrentID != "" {
		layer := LayerOf(v.Map, v.CurrentID)
		if len(layer) > 0 {
			label = LayerLabel(v.Map, layer, v.CurrentID)
		}
		if r, ok := v.Map.Rooms[v.CurrentID]; ok {
			currentName = r.Name
		}
	}
	return []string{
		padRight(label, v.PaneWidth),
		padRight(currentName, v.PaneWidth),
	}
}

// footerLine returns the bridge / dig hint footer (one line).
func footerLine(v View) string {
	// Concrete logic added in Task 13. For now, "(no layer bridges)" is the
	// safe default that satisfies fallback tests.
	return padRight("(no layer bridges)", v.PaneWidth)
}

const (
	glyphRoom        = '■'
	glyphCurrentRoom = '▣'
	glyphPlaceholder = '□'
	glyphHLink       = '─'
	glyphVLink       = '│'
	glyphDiagNESW    = '╱'
	glyphDiagNWSE    = '╲'
)

// orderedLayerIDs returns layer IDs in ascending order (deterministic).
func orderedLayerIDs(layer map[string]struct{}) []string {
	out := make([]string, 0, len(layer))
	for id := range layer {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// bodyLines builds the map drawing area as a slice of row strings.
func bodyLines(v View, rows int) []string {
	if rows <= 0 {
		return []string{}
	}
	cols := v.PaneWidth
	grid := newGrid(cols, rows)

	// Fallback strings keep the previous behaviour for invalid input.
	if v.Map == nil {
		grid.setLine(0, "(no map — try /map create)")
		return grid.toLines()
	}
	current, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		grid.setLine(0, "(current room missing from map)")
		return grid.toLines()
	}

	// Determine layer membership.
	layer := LayerOf(v.Map, v.CurrentID)
	if len(layer) == 0 {
		layer = map[string]struct{}{v.CurrentID: {}}
	}

	// Anchor: render coordinates relative to the current room.
	cx, cy := current.X, current.Y
	centerCol := (cols / 2) &^ 1 // snap to even
	centerRow := (rows / 2) &^ 1

	// Track collisions: key is "(rx,ry)" string, value is occurrence index.
	collisions := map[[2]int]int{}
	overlapFlagged := map[[2]int]bool{}
	for _, id := range orderedLayerIDs(layer) {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		key := [2]int{r.X, r.Y}
		nudge := collisions[key]
		collisions[key] = nudge + 1

		col := centerCol + 2*(r.X-cx) - v.PanOffset.Col + nudge
		row := centerRow + 2*(cy-r.Y) - v.PanOffset.Row
		if col < 0 || col >= cols || row < 0 || row >= rows {
			continue
		}
		switch {
		case id == v.CurrentID:
			grid.set(col, row, glyphCurrentRoom)
		case placeholderName(r.Name):
			grid.set(col, row, glyphPlaceholder)
		default:
			grid.set(col, row, glyphRoom)
		}

		if nudge >= 1 && !overlapFlagged[key] {
			grid.set(col+1, row, '!')
			overlapFlagged[key] = true
		}
	}

	// Draw links — one pass per direction. To avoid double-drawing, only
	// emit a link when the source ID is alphabetically smaller than the
	// destination ID. Connectivity rule: the destination must be in the
	// layer AND its X/Y must match the expected offset.
	for id := range layer {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		for d, otherID := range r.Exits {
			if id >= otherID {
				continue
			}
			other, ok := v.Map.Rooms[otherID]
			if !ok {
				continue
			}
			if _, inLayer := layer[otherID]; !inLayer {
				continue
			}
			dx, dy, ok := cardinalOffset(d)
			if !ok {
				continue
			}
			if other.X != r.X+dx || other.Y != r.Y+dy {
				continue
			}
			drawLink(grid, r.X-cx, cy-r.Y, dx, -dy, centerCol, centerRow, v.PanOffset)
		}
	}

	return grid.toLines()
}

// cardinalOffset returns the X/Y deltas a direction implies, plus an "ok"
// flag for the eight in-layer directions. Up/Down/In/Out return false.
func cardinalOffset(d mapper.Direction) (int, int, bool) {
	switch d {
	case mapper.North:
		return 0, 1, true
	case mapper.South:
		return 0, -1, true
	case mapper.East:
		return 1, 0, true
	case mapper.West:
		return -1, 0, true
	case mapper.NorthEast:
		return 1, 1, true
	case mapper.NorthWest:
		return -1, 1, true
	case mapper.SouthEast:
		return 1, -1, true
	case mapper.SouthWest:
		return -1, -1, true
	}
	return 0, 0, false
}

// drawLink writes the gutter glyph between (relX, relY) and (relX+dx, relY+dy)
// in coord-space. screenDX/screenDY are the pre-flipped screen-space offsets
// (Y inverted). The gutter glyph sits at the half-step.
func drawLink(g *grid, relX, relY, dx, screenDY int, centerCol, centerRow int, pan Point) {
	// Source screen position.
	sCol := centerCol + 2*relX - pan.Col
	sRow := centerRow + 2*relY - pan.Row
	gCol := sCol + dx
	gRow := sRow + screenDY
	switch {
	case dx != 0 && screenDY == 0:
		g.set(gCol, gRow, glyphHLink)
	case dx == 0 && screenDY != 0:
		g.set(gCol, gRow, glyphVLink)
	case dx > 0 && screenDY < 0, dx < 0 && screenDY > 0:
		g.set(gCol, gRow, glyphDiagNESW) // ╱
	case dx > 0 && screenDY > 0, dx < 0 && screenDY < 0:
		g.set(gCol, gRow, glyphDiagNWSE) // ╲
	}
}

// grid is a 2-D rune buffer that fills with spaces and renders to lines.
type grid struct {
	cells [][]rune
	cols  int
	rows  int
}

func newGrid(cols, rows int) *grid {
	g := &grid{cols: cols, rows: rows, cells: make([][]rune, rows)}
	for r := range g.cells {
		g.cells[r] = make([]rune, cols)
		for c := range g.cells[r] {
			g.cells[r][c] = ' '
		}
	}
	return g
}

func (g *grid) set(col, row int, r rune) {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return
	}
	g.cells[row][col] = r
}

func (g *grid) get(col, row int) rune {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return 0
	}
	return g.cells[row][col]
}

func (g *grid) setLine(row int, s string) {
	if row < 0 || row >= g.rows {
		return
	}
	r := []rune(s)
	for i := 0; i < g.cols; i++ {
		if i < len(r) {
			g.cells[row][i] = r[i]
		} else {
			g.cells[row][i] = ' '
		}
	}
}

func (g *grid) toLines() []string {
	out := make([]string, g.rows)
	for r := range out {
		out[r] = string(g.cells[r])
	}
	return out
}
