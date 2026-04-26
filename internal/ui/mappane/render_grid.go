package mappane

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

	// Place the rooms.
	for id := range layer {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		col := centerCol + 2*(r.X-cx) - v.PanOffset.Col
		row := centerRow + 2*(cy-r.Y) - v.PanOffset.Row // y inverted
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
	}

	return grid.toLines()
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
