package mappane

import (
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
