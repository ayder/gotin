package mappane

import (
	"fmt"
	"sort"

	"github.com/ayder/gotin/internal/mapper"
)

// cardinalDirs is the set of exits that stay within a layer.
var cardinalDirs = []mapper.Direction{
	mapper.North, mapper.South, mapper.East, mapper.West,
	mapper.NorthEast, mapper.NorthWest, mapper.SouthEast, mapper.SouthWest,
}

// sameLayer reports whether a and b are in the same connected component.
func sameLayer(m *mapper.Map, a, b string) bool {
	if a == "" || b == "" || a == b {
		return true
	}
	la := LayerOf(m, a)
	if _, ok := la[b]; ok {
		return true
	}
	return false
}

// layerMembership groups weakly connected compass components. A one-way
// passage is visible from either endpoint without inventing a reverse exit.
// Up/down and named portals never join planar layers.
func layerMembership(m *mapper.Map) map[string]string {
	membership := map[string]string{}
	if m == nil {
		return membership
	}
	adjacency := map[string][]string{}
	var ids []string
	for id, r := range m.Rooms {
		if r == nil {
			continue
		}
		ids = append(ids, id)
		for _, d := range cardinalDirs {
			to, ok := r.Exits[d]
			if !ok || m.Rooms[to] == nil {
				continue
			}
			adjacency[id] = append(adjacency[id], to)
			adjacency[to] = append(adjacency[to], id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, seen := membership[id]; seen {
			continue
		}
		membership[id] = id
		queue := []string{id}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			for _, to := range adjacency[at] {
				if _, seen := membership[to]; seen {
					continue
				}
				membership[to] = id
				queue = append(queue, to)
			}
		}
	}
	return membership
}

func LayerOf(m *mapper.Map, start string) map[string]struct{} {
	out := map[string]struct{}{}
	membership := layerMembership(m)
	layer, ok := membership[start]
	if !ok {
		return out
	}
	for id, key := range membership {
		if key == layer {
			out[id] = struct{}{}
		}
	}
	return out
}

func AllLayers(m *mapper.Map) []string {
	seen := map[string]bool{}
	var layers []string
	for _, layer := range layerMembership(m) {
		if !seen[layer] {
			seen[layer] = true
			layers = append(layers, layer)
		}
	}
	sort.Strings(layers)
	return layers
}

// placeholderName returns true for names that should not vote in the
// layer-label heuristic.
func placeholderName(s string) bool {
	switch s {
	case "", "New Room", "Start":
		return true
	}
	return false
}

// LayerLabel produces the human-friendly heading rendered above the map
// pane body. layer must be the result of LayerOf(m, currentID); currentID
// is used as the final fallback when no real names are available.
func LayerLabel(m *mapper.Map, layer map[string]struct{}, currentID string) string {
	if m == nil || len(layer) == 0 {
		return "Unnamed Layer"
	}
	counts := map[string]int{}
	for id := range layer {
		r, ok := m.Rooms[id]
		if !ok {
			continue
		}
		if placeholderName(r.Name) {
			continue
		}
		counts[r.Name]++
	}

	primary := ""
	if len(counts) > 0 {
		// Mode wins; ties broken alphabetically.
		var names []string
		for n := range counts {
			names = append(names, n)
		}
		sort.Strings(names)
		bestCount := -1
		for _, n := range names {
			if counts[n] > bestCount {
				bestCount = counts[n]
				primary = n
			}
		}
	}
	if primary == "" {
		if cur, ok := m.Rooms[currentID]; ok && !placeholderName(cur.Name) {
			primary = cur.Name
		}
	}
	if primary == "" {
		primary = "Unnamed Layer"
	}

	// Z annotation.
	zSet := map[int]struct{}{}
	for id := range layer {
		if r, ok := m.Rooms[id]; ok {
			zSet[r.Z] = struct{}{}
		}
	}
	if len(zSet) > 1 {
		return primary + " [Z=mixed]"
	}
	for z := range zSet {
		if z != 0 {
			return fmt.Sprintf("%s [Z=%d]", primary, z)
		}
	}
	return primary
}
