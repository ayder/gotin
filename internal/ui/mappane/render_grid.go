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

// bodyLines returns rows rows of body content, padded to v.PaneWidth. The
// first cut renders fallbacks only; cardinal/diagonal drawing is layered in
// in subsequent tasks.
func bodyLines(v View, rows int) []string {
	out := make([]string, rows)
	for i := range out {
		out[i] = padRight("", v.PaneWidth)
	}
	if rows == 0 {
		return out
	}
	switch {
	case v.Map == nil:
		out[0] = padRight("(no map — try /map create)", v.PaneWidth)
	case v.CurrentID == "":
		out[0] = padRight("(current room missing from map)", v.PaneWidth)
	case v.Map.Rooms[v.CurrentID] == nil:
		out[0] = padRight("(current room missing from map)", v.PaneWidth)
	}
	return out
}
