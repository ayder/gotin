package mappane

import (
	"sort"
	"strings"

	"github.com/ayder/gotin/internal/mapper"
)

// layerHeaderLines returns the two header rows: layer label + current room
// name. Both are padded to PaneWidth.
func layerHeaderLines(v View) []string {
	label := "Unnamed Layer"
	currentName := ""
	if v.Map != nil {
		anchorID := v.CurrentID
		remote := false
		if v.LayerKey != "" && v.LayerKey != v.CurrentID {
			if _, ok := v.Map.Rooms[v.LayerKey]; ok && !sameLayer(v.Map, v.CurrentID, v.LayerKey) {
				anchorID = v.LayerKey
				remote = true
			}
		}
		if anchorID != "" {
			layer := LayerOf(v.Map, anchorID)
			if len(layer) > 0 {
				label = LayerLabel(v.Map, layer, anchorID)
			}
			if remote {
				label += " (viewing remote)"
			}
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

// footerLine returns the bridge / dig hint footer (single padded row).
//
//	↑ <name> | ↓ <name> | ▶ <name> | ◀ <name> | dig: <missing dirs>
//
// Only directions that exist on the current room appear in the bridge
// segment. The dig segment lists the subset of {u, d, in, out} that has no
// exit yet. When all bridges are linked, dig is omitted; when none are
// linked and the current room is unknown, the literal `(no layer bridges)`
// is shown.
func footerLine(v View) string {
	if v.Map == nil {
		return padRight("(no layer bridges)", v.PaneWidth)
	}
	curr, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		return padRight("(no layer bridges)", v.PaneWidth)
	}

	type pair struct {
		dir    mapper.Direction
		arrow  string
		digTok string
	}
	pairs := []pair{
		{mapper.Up, "↑", "u"},
		{mapper.Down, "↓", "d"},
		{mapper.In, "▶", "in"},
		{mapper.Out, "◀", "out"},
	}

	var bridgeSegs []string
	var digMissing []string
	for _, p := range pairs {
		if dest, has := curr.Exits[p.dir]; has {
			name := dest
			if r, ok := v.Map.Rooms[dest]; ok && r.Name != "" {
				name = r.Name
			}
			bridgeSegs = append(bridgeSegs, p.arrow+" "+name)
		} else {
			digMissing = append(digMissing, p.digTok)
		}
	}

	var parts []string
	if len(bridgeSegs) > 0 {
		parts = append(parts, strings.Join(bridgeSegs, " | "))
	}
	if len(digMissing) > 0 {
		parts = append(parts, "dig: "+strings.Join(digMissing, "/"))
	}
	if len(parts) == 0 {
		return padRight("(no layer bridges)", v.PaneWidth)
	}
	return padRight(strings.Join(parts, " | "), v.PaneWidth)
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
	if _, ok := v.Map.Rooms[v.CurrentID]; !ok {
		grid.setLine(0, "(current room missing from map)")
		return grid.toLines()
	}

	// Determine which room id anchors the layer being rendered.
	anchorID := v.CurrentID
	if v.LayerKey != "" && v.LayerKey != v.CurrentID {
		if _, ok := v.Map.Rooms[v.LayerKey]; ok && !sameLayer(v.Map, v.CurrentID, v.LayerKey) {
			anchorID = v.LayerKey
		}
	}
	layer := LayerOf(v.Map, anchorID)
	if len(layer) == 0 {
		layer = map[string]struct{}{anchorID: {}}
	}

	// Anchor the body on the chosen layer's anchor room. When viewing remote,
	// the current room is not in `layer`, so no current-room glyph is drawn
	// — that is the desired behaviour.
	anchor := v.Map.Rooms[anchorID]
	cx, cy := anchor.X, anchor.Y
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

	// Draw links between rooms in the same layer. To avoid double-drawing,
	// only emit a link when the source ID is alphabetically smaller than the
	// destination ID. Non-step-adjacent cells are connected with a rasterised
	// long line.
	for _, id := range orderedLayerIDs(layer) {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		for _, d := range sortedMappaneDirs(r.Exits) {
			otherID := r.Exits[d]
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
			if _, _, ok := cardinalOffset(d); !ok {
				continue
			}
			sCol := centerCol + 2*(r.X-cx) - v.PanOffset.Col
			sRow := centerRow + 2*(cy-r.Y) - v.PanOffset.Row
			tCol := centerCol + 2*(other.X-cx) - v.PanOffset.Col
			tRow := centerRow + 2*(cy-other.Y) - v.PanOffset.Row
			drawLongLine(grid, sCol, sRow, tCol, tRow)
		}
	}

	// Bridge overlays handled in helper for clarity.
	drawBridgeOverlays(grid, v, layer, cx, cy, centerCol, centerRow, cols, rows)

	// Clipped current-room indicator: if the current room exists in the
	// rendered layer but its placement falls outside the body, place a '*'
	// at the body edge nearest the room's logical direction.
	if curr, ok := v.Map.Rooms[v.CurrentID]; ok {
		if _, isInLayer := layer[v.CurrentID]; isInLayer {
			col := centerCol + 2*(curr.X-cx) - v.PanOffset.Col
			row := centerRow + 2*(cy-curr.Y) - v.PanOffset.Row
			if col < 0 || col >= cols || row < 0 || row >= rows {
				eCol := col
				eRow := row
				if eCol < 0 {
					eCol = 0
				} else if eCol >= cols {
					eCol = cols - 1
				}
				if eRow < 0 {
					eRow = 0
				} else if eRow >= rows {
					eRow = rows - 1
				}
				grid.set(eCol, eRow, '*')
			}
		}
	}

	return grid.toLines()
}

// drawBridgeOverlays writes the up/down/in/out arrow glyphs into the
// gutters around the current-room cell, but only when the current room is
// part of the rendered layer (otherwise we are viewing a remote layer and
// the player's room belongs elsewhere). A glyph is written only when its
// gutter cell is still ' ' — cardinal/diagonal links keep precedence.
func drawBridgeOverlays(g *grid, v View, layer map[string]struct{}, cx, cy, centerCol, centerRow, cols, rows int) {
	if _, currInLayer := layer[v.CurrentID]; !currInLayer {
		return
	}
	curr, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		return
	}
	curCol := centerCol + 2*(curr.X-cx) - v.PanOffset.Col
	curRow := centerRow + 2*(cy-curr.Y) - v.PanOffset.Row
	type slot struct {
		dCol, dRow int
		glyph      rune
	}
	bridges := []struct {
		dir  mapper.Direction
		slot slot
	}{
		{mapper.Up, slot{0, -1, '↑'}},
		{mapper.Down, slot{0, +1, '↓'}},
		{mapper.In, slot{+1, 0, '▶'}},
		{mapper.Out, slot{-1, 0, '◀'}},
	}
	for _, br := range bridges {
		if _, has := curr.Exits[br.dir]; !has {
			continue
		}
		tCol := curCol + br.slot.dCol
		tRow := curRow + br.slot.dRow
		if tCol < 0 || tCol >= cols || tRow < 0 || tRow >= rows {
			continue
		}
		if g.get(tCol, tRow) == ' ' {
			g.set(tCol, tRow, br.slot.glyph)
		}
	}
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

func sortedMappaneDirs(exits map[mapper.Direction]string) []mapper.Direction {
	dirs := make([]mapper.Direction, 0, len(exits))
	for d := range exits {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return string(dirs[i]) < string(dirs[j]) })
	return dirs
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

func drawLongLine(g *grid, sCol, sRow, tCol, tRow int) {
	dx := tCol - sCol
	dy := tRow - sRow
	if dx == 0 && dy == 0 {
		return
	}
	var glyph rune
	switch {
	case dx == 0:
		glyph = glyphVLink
	case dy == 0:
		glyph = glyphHLink
	case (dx > 0) == (dy < 0):
		glyph = glyphDiagNESW
	default:
		glyph = glyphDiagNWSE
	}

	adx, ady := absMappaneInt(dx), absMappaneInt(dy)
	sx, sy := signMappaneInt(dx), signMappaneInt(dy)
	col, row := sCol, sRow
	if adx >= ady {
		err := 0
		for i := 0; i < adx; i++ {
			col += sx
			err += ady
			if 2*err >= adx {
				row += sy
				err -= adx
			}
			if col == tCol && row == tRow {
				break
			}
			if g.get(col, row) == ' ' {
				g.set(col, row, glyph)
			}
		}
		return
	}
	err := 0
	for i := 0; i < ady; i++ {
		row += sy
		err += adx
		if 2*err >= ady {
			col += sx
			err -= ady
		}
		if col == tCol && row == tRow {
			break
		}
		if g.get(col, row) == ' ' {
			g.set(col, row, glyph)
		}
	}
}

func absMappaneInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func signMappaneInt(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
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
